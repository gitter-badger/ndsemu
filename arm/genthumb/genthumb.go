package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

var filename = flag.String("filename", "-", "output filename")

type Generator struct {
	io.Writer
}

func (g *Generator) WriteHeader() {
	fmt.Fprintf(g, "// Generated on %v\n", time.Now())
	fmt.Fprintf(g, "package arm\n")

	fmt.Fprintf(g, "var opThumbTable = [256]func(*Cpu, uint16) {\n")
	for i := 0; i < 256; i++ {
		fmt.Fprintf(g, "(*Cpu).opThumb%02X,\n", i)
	}
	fmt.Fprintf(g, "}\n")

	fmt.Fprintf(g, "var opThumbAluTable = [16]func(*Cpu, uint16) {\n")
	for i := 0; i < 16; i++ {
		fmt.Fprintf(g, "(*Cpu).opThumbAlu%02X,\n", i)
	}
	fmt.Fprintf(g, "}\n")
}

func (g *Generator) WriteFooter() {

}

func (g *Generator) writeOpHeader(op uint16) {
	fmt.Fprintf(g, "func (cpu *Cpu) opThumb%02X(op uint16) {\n", (op>>8)&0xFF)
}
func (g *Generator) writeOpFooter(op uint16) {
	fmt.Fprintf(g, "}\n\n")
}
func (g *Generator) writeOpAluHeader(op uint16) {
	fmt.Fprintf(g, "func (cpu *Cpu) opThumbAlu%02X(op uint16) {\n", (op>>6)&0xF)
}
func (g *Generator) writeOpAluFooter(op uint16) {
	fmt.Fprintf(g, "}\n\n")
}

func (g *Generator) writeOpInvalid(op uint16, msg string) {
	fmt.Fprintf(g, "cpu.InvalidOpThumb(op, %q)\n", msg)
}

var f1name = [3]string{"LSL", "LSR", "ASR"}

func (g *Generator) writeOpF1Shift(op uint16) {
	opcode := (op >> 11) & 3

	fmt.Fprintf(g, "// %s\n", f1name[opcode])

	fmt.Fprintf(g, "rsx := (op>>3)&7\n")
	fmt.Fprintf(g, "rdx := op&7\n")
	fmt.Fprintf(g, "offset := (op>>6)&0x1F\n")
	fmt.Fprintf(g, "rs := uint32(cpu.Regs[rsx])\n")

	switch opcode {
	case 0: // LSL
		fmt.Fprintf(g, "if offset != 0 { cpu.Cpsr.SetC(rs & (1<<(32-offset)) != 0) }\n")
		fmt.Fprintf(g, "res := rs << offset\n")
	case 1: // LSR
		fmt.Fprintf(g, "if offset == 0 { offset = 32 }\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(rs & (1<<(offset-1)) != 0)\n")
		fmt.Fprintf(g, "res := rs >> offset\n")
	case 2: // ASR
		fmt.Fprintf(g, "if offset == 0 { offset = 32 }\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(rs & (1<<(offset-1)) != 0)\n")
		fmt.Fprintf(g, "res := uint32(int32(rs) >> offset)\n")
	default:
		panic("unreachable")
	}

	fmt.Fprintf(g, "cpu.Cpsr.SetNZ(res)\n")
	fmt.Fprintf(g, "cpu.Regs[rdx] = reg(res)\n")
}

var f2name = [4]string{"ADD", "SUB", "ADD #nn", "SUB #nn"}

func (g *Generator) writeOpF2Add(op uint16) {
	opcode := (op >> 9) & 3
	imm := opcode&2 != 0

	fmt.Fprintf(g, "// %s\n", f2name[opcode])

	fmt.Fprintf(g, "rsx := (op>>3)&7\n")
	fmt.Fprintf(g, "rdx := op&7\n")
	fmt.Fprintf(g, "rs := uint32(cpu.Regs[rsx])\n")

	if imm {
		fmt.Fprintf(g, "val := uint32((op>>6)&7)\n")
	} else {
		fmt.Fprintf(g, "rnx := (op>>6)&7\n")
		fmt.Fprintf(g, "val := uint32(cpu.Regs[rnx])\n")
	}

	switch opcode {
	case 0, 2: // ADD
		fmt.Fprintf(g, "res := rs + val\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(res<rs)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetVAdd(rs, val, res)\n")
	case 1, 3: // SUB
		fmt.Fprintf(g, "res := rs - val\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(res>rs)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetVSub(rs, val, res)\n")
	}

	fmt.Fprintf(g, "cpu.Cpsr.SetNZ(res)\n")
	fmt.Fprintf(g, "cpu.Regs[rdx] = reg(res)\n")
}

var f3name = [4]string{"MOV", "CMP", "ADD", "SUB"}

func (g *Generator) writeOpF3AluImm(op uint16) {
	opcode := (op >> 11) & 3
	rdx := (op >> 8) & 7

	fmt.Fprintf(g, "// %s\n", f3name[opcode])

	test := false
	fmt.Fprintf(g, "imm := uint32(op&0xFF)\n")
	switch opcode {
	case 0: // MOV
		fmt.Fprintf(g, "res := imm\n")
	case 2: // ADD
		fmt.Fprintf(g, "rd := uint32(cpu.Regs[%d])\n", rdx)
		fmt.Fprintf(g, "res := rd + imm\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(res<rd)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetVAdd(rd, imm, res)\n")
	case 1: // CMP
		test = true
		fallthrough
	case 3: // SUB
		fmt.Fprintf(g, "rd := uint32(cpu.Regs[%d])\n", rdx)
		fmt.Fprintf(g, "res := rd - imm\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(res>rd)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetVSub(rd, imm, res)\n")
	default:
		panic("unreachable")
	}
	fmt.Fprintf(g, "cpu.Cpsr.SetNZ(res)\n")
	if !test {
		fmt.Fprintf(g, "cpu.Regs[%d] = reg(res)\n", rdx)
	}
}

func (g *Generator) writeOpF4Alu(op uint16) {
	// F4 is the only format of opcodes where the real opcode is encoded in
	// bits below the 8th, so our dispatch table can't properly differentiate
	// between all instructions. Instead of doing all the decoding at runtime,
	// we do a second-level dispatching:
	fmt.Fprintf(g, "opThumbAluTable[(op>>6)&0xF](cpu, op)\n")
}

var f5name = [4]string{"ADD(h)", "CMP(h)", "MOV(h)", "BX/BLX"}

func (g *Generator) writeOpF5HiReg(op uint16) {
	opcode := (op >> 8) & 3

	fmt.Fprintf(g, "// %s\n", f5name[opcode])
	fmt.Fprintf(g, "rdx := (op&7) | (op&0x80)>>4\n")
	fmt.Fprintf(g, "rsx := ((op>>3)&0xF)\n")
	fmt.Fprintf(g, "rs := uint32(cpu.Regs[rsx])\n")

	switch opcode {
	case 0: // ADD
		// NOTE: this does not affect flags (!)
		fmt.Fprintf(g, "rd := uint32(cpu.Regs[rdx])\n")
		fmt.Fprintf(g, "cpu.Regs[rdx] = reg(rd+rs)\n")
	case 1: // CMP
		fmt.Fprintf(g, "rd := uint32(cpu.Regs[rdx])\n")
		fmt.Fprintf(g, "res := rd-rs\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetNZ(res)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(res>rd)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetVSub(rd, rs, res)\n")
	case 2: // MOV
		fmt.Fprintf(g, "cpu.Regs[rdx] = reg(rs)\n")
	case 3: // BX/BLX
		fmt.Fprintf(g, "if op&0x80 != 0 { cpu.Regs[14] = cpu.Regs[15]+1 }\n")
		fmt.Fprintf(g, "cpu.pc = reg(rs)\n")
		fmt.Fprintf(g, "if rs&1==0 { cpu.Cpsr.SetT(false); cpu.pc &^= 3 }\n")
		fmt.Fprintf(g, "_=rdx\n")
	default:
		panic("unreachable")
	}
}

func (g *Generator) writeOpF6LdrPc(op uint16) {
	rdx := (op >> 8) & 7
	fmt.Fprintf(g, "// LDR PC\n")
	fmt.Fprintf(g, "pc := uint32(cpu.Regs[15]) &^ 2\n")
	fmt.Fprintf(g, "pc += uint32((op & 0xFF)*4)\n")
	fmt.Fprintf(g, "cpu.Regs[%d] = reg(cpu.opRead32(pc))\n", rdx)
}

var f7name = [4]string{"STR", "STRB", "LDR", "LDRB"}
var f8name = [4]string{"STRH", "LDSB", "LDRH", "LDSH"}

func (g *Generator) writeOpF7F8LdrStr(op uint16) {
	opcode := (op >> 10) & 3
	f8 := op&(1<<9) != 0

	if !f8 {
		fmt.Fprintf(g, "// %s\n", f7name[opcode])
	} else {
		fmt.Fprintf(g, "// %s\n", f8name[opcode])
	}

	fmt.Fprintf(g, "rox := (op>>6)&7\n")
	fmt.Fprintf(g, "rbx := (op>>3)&7\n")
	fmt.Fprintf(g, "rdx := op&7\n")
	fmt.Fprintf(g, "addr := uint32(cpu.Regs[rbx] + cpu.Regs[rox])\n")

	if !f8 {
		switch opcode {
		case 0: // STR
			fmt.Fprintf(g, "cpu.opWrite32(addr, uint32(cpu.Regs[rdx]))\n")
		case 1: // STRB
			fmt.Fprintf(g, "cpu.opWrite8(addr, uint8(cpu.Regs[rdx]))\n")
		case 2: // LDR
			fmt.Fprintf(g, "cpu.Regs[rdx] = reg(cpu.opRead32(addr))\n")
		case 3: // LDRB
			fmt.Fprintf(g, "cpu.Regs[rdx] = reg(cpu.opRead8(addr))\n")
		default:
			panic("unreachable")
		}
	} else {
		switch opcode {
		case 0: // STRH
			fmt.Fprintf(g, "cpu.opWrite16(addr, uint16(cpu.Regs[rdx]))\n")
		case 1: // LDSB
			fmt.Fprintf(g, "cpu.Regs[rdx] = reg(int8(cpu.opRead8(addr)))\n")
		case 2: // LDRH
			fmt.Fprintf(g, "cpu.Regs[rdx] = reg(cpu.opRead16(addr))\n")
		case 3: // LDSH
			fmt.Fprintf(g, "cpu.Regs[rdx] = reg(int16(cpu.opRead16(addr)))\n")
		default:
			panic("unreachable")
		}
	}
}

var f9name = [4]string{"STR #nn", "LDR #nn", "STRB #nn", "LDRB #nn"}

func (g *Generator) writeOpF9Strb(op uint16) {
	opcode := (op >> 11) & 3
	fmt.Fprintf(g, "// %s\n", f9name[opcode])
	fmt.Fprintf(g, "offset := uint32((op>>6)&0x1F)\n")
	fmt.Fprintf(g, "rbx := (op>>3)&0x7\n")
	fmt.Fprintf(g, "rdx := op&0x7\n")
	fmt.Fprintf(g, "rb := uint32(cpu.Regs[rbx])\n")
	switch opcode {
	case 0: // STR
		fmt.Fprintf(g, "offset *= 4\n")
		fmt.Fprintf(g, "rd := uint32(cpu.Regs[rdx])\n")
		fmt.Fprintf(g, "cpu.opWrite32(rb+offset, rd)\n")
	case 1: // LDR
		fmt.Fprintf(g, "offset *= 4\n")
		fmt.Fprintf(g, "cpu.Regs[rdx] = reg(cpu.opRead32(rb+offset))\n")
	case 2: // STRB
		fmt.Fprintf(g, "rd := uint8(cpu.Regs[rdx])\n")
		fmt.Fprintf(g, "cpu.opWrite8(rb+offset, rd)\n")
	case 3: // LDRB
		fmt.Fprintf(g, "cpu.Regs[rdx] = reg(cpu.opRead8(rb+offset))\n")
	}
}

var f10name = [4]string{"STRH #nn", "LDRH #nn"}

func (g *Generator) writeOpF10Strh(op uint16) {
	opcode := (op >> 11) & 1
	fmt.Fprintf(g, "// %s\n", f10name[opcode])
	fmt.Fprintf(g, "offset := uint32((op>>6)&0x1F)\n")
	fmt.Fprintf(g, "rbx := (op>>3)&0x7\n")
	fmt.Fprintf(g, "rdx := op&0x7\n")
	fmt.Fprintf(g, "rb := uint32(cpu.Regs[rbx])\n")

	switch opcode {
	case 0: // STRH
		fmt.Fprintf(g, "offset *= 2\n")
		fmt.Fprintf(g, "rd := uint16(cpu.Regs[rdx])\n")
		fmt.Fprintf(g, "cpu.opWrite16(rb+offset, rd)\n")
	case 1: // LDRH
		fmt.Fprintf(g, "offset *= 2\n")
		fmt.Fprintf(g, "cpu.Regs[rdx] = reg(cpu.opRead16(rb+offset))\n")
	}
}

func (g *Generator) writeOpF14PushPop(op uint16) {
	pop := (op>>11)&1 != 0

	if pop {
		fmt.Fprintf(g, "// POP\n")
	} else {
		fmt.Fprintf(g, "// PUSH\n")
		fmt.Fprintf(g, "count := popcount8(uint8(op&0x1F))\n")
	}

	fmt.Fprintf(g, "sp := uint32(cpu.Regs[13])\n")
	if !pop {
		fmt.Fprintf(g, "sp -= uint32(count*4)\n")
		fmt.Fprintf(g, "cpu.Regs[13] = reg(sp)\n")
	}

	for i := 0; i < 9; i++ {
		fmt.Fprintf(g, "if (op>>%d)&1 != 0 {\n", i)
		regnum := i
		if i == 8 {
			if pop {
				regnum = 15
			} else {
				regnum = 14
			}
		}
		if pop {
			fmt.Fprintf(g, "  cpu.Regs[%d] = reg(cpu.opRead32(sp))\n", regnum)
		} else {
			fmt.Fprintf(g, "  cpu.opWrite32(sp, uint32(cpu.Regs[%d]))\n", regnum)
		}
		fmt.Fprintf(g, "  sp += 4\n")
		fmt.Fprintf(g, "}\n")
	}

	if pop {
		fmt.Fprintf(g, "cpu.Regs[13] = reg(sp)\n")
	}
}

var f16name = [16]string{
	"BEQ", "BNE", "BCS/BHS", "BCC/BLO", "BMI", "BPL", "BVS", "BVC",
	"BHI", "BLS", "BGE", "BLT", "BGT", "BLE", "B undefined", "SWI",
}

var f16cond = [14]string{
	"cpu.Cpsr.Z()",  // BEQ
	"!cpu.Cpsr.Z()", // BNE
	"cpu.Cpsr.C()",  // BHS
	"!cpu.Cpsr.C()", // BLO
	"cpu.Cpsr.N()",  // BMI
	"!cpu.Cpsr.N()", // BPL
	"cpu.Cpsr.V()",  // BVS
	"!cpu.Cpsr.V()", // BVC

	"cpu.Cpsr.C() && !cpu.Cpsr.Z()", // BHI
	"!cpu.Cpsr.C() || cpu.Cpsr.Z()", // BLS

	"cpu.Cpsr.N() == cpu.Cpsr.V()", // BGE
	"cpu.Cpsr.N() != cpu.Cpsr.V()", // BLT

	"!cpu.Cpsr.Z() && cpu.Cpsr.N() == cpu.Cpsr.V()", // BGT
	"cpu.Cpsr.Z() || cpu.Cpsr.N() != cpu.Cpsr.V()",  // BLE
}

func (g *Generator) writeOpF16BranchCond(op uint16) {
	opcode := (op >> 8) & 0xF

	fmt.Fprintf(g, "// %s\n", f16name[opcode])
	if opcode == 14 {
		g.writeOpInvalid(op, "invalid F16 with opcode==14")
		return
	}
	if opcode == 15 {
		g.writeOpInvalid(op, "F16 SWI unimplemented")
		return
	}

	fmt.Fprintf(g, "if %s {\n", f16cond[opcode])
	fmt.Fprintf(g, "offset := int8(uint8(op&0xFF)) * 2\n")
	fmt.Fprintf(g, "offset32 := int32(offset)\n")
	fmt.Fprintf(g, "cpu.pc = cpu.Regs[15]+reg(offset32)\n")
	fmt.Fprintf(g, "}\n")
}

func (g *Generator) writeOpF19LongBranch1(op uint16) {
	fmt.Fprintf(g, "// BL/BLX step 1\n")
	fmt.Fprintf(g, "cpu.Regs[14] = cpu.Regs[15] + reg(int32(uint32(op&0x7FF)<<23)>>11)\n")
}

func (g *Generator) writeOpF19LongBranch2(op uint16) {
	blx := (op>>12)&1 == 0
	if blx {
		fmt.Fprintf(g, "// BLX step 2\n")
	} else {
		fmt.Fprintf(g, "// BL step 2\n")
	}
	fmt.Fprintf(g, "cpu.pc = cpu.Regs[14] + reg((op&0x7FF)<<1)\n")
	fmt.Fprintf(g, "cpu.Regs[14] = (cpu.Regs[15]-2) | 1\n")
	if blx {
		fmt.Fprintf(g, "cpu.Cpsr.SetT(false)\n")
	}
}

var opaluname = [16]string{
	"AND", "EOR", "LSL", "LSR", "ASR", "ADC", "SBC", "ROR",
	"TST", "NEG", "CMP", "CMN", "ORR", "MUL", "BIC", "MVN",
}

func (g *Generator) WriteAluOp(op uint16) {
	if op>>10 != 0x10 {
		panic("invalid ALU opcode")
	}
	opcode := (op >> 6) & 0xF

	g.writeOpAluHeader(op)
	fmt.Fprintf(g, "// %s\n", opaluname[opcode])
	fmt.Fprintf(g, "rsx := (op>>3)&0x7\n")
	fmt.Fprintf(g, "rs := uint32(cpu.Regs[rsx])\n")
	fmt.Fprintf(g, "rdx := op&0x7\n")
	if opcode != 9 && opcode != 0xF {
		fmt.Fprintf(g, "rd := uint32(cpu.Regs[rdx])\n")
	}

	test := false
	switch opcode {
	case 8: // TST
		test = true
		fallthrough
	case 0: // AND
		fmt.Fprintf(g, "res := rd & rs\n")
	case 1: // EOR
		fmt.Fprintf(g, "res := rd ^ rs\n")
	case 2: // LSL
		fmt.Fprintf(g, "rot := (rs&0xFF)\n")
		fmt.Fprintf(g, "if rot != 0 { cpu.Cpsr.SetC(rd & (1<<(32-rot)) != 0) }\n")
		fmt.Fprintf(g, "res := rd << rot\n")
	case 3: // LSR
		fmt.Fprintf(g, "rot := (rs&0xFF)\n")
		fmt.Fprintf(g, "if rot != 0 { cpu.Cpsr.SetC(rd & (1<<(rot-1)) != 0) }\n")
		fmt.Fprintf(g, "res := rd >> rot\n")
	case 4: // ASR
		fmt.Fprintf(g, "rot := (rs&0xFF)\n")
		fmt.Fprintf(g, "if rot != 0 { cpu.Cpsr.SetC(rd & (1<<(rot-1)) != 0) }\n")
		fmt.Fprintf(g, "res := uint32(int32(rd) >> rot)\n")
	case 5: // ADC
		fmt.Fprintf(g, "cf := cpu.Cpsr.CB()\n")
		fmt.Fprintf(g, "res := rd + rs\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(res<rd)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetVAdd(rd, rs, res)\n")
		fmt.Fprintf(g, "res += cf\n")
	case 6: // SBC
		fmt.Fprintf(g, "cf := cpu.Cpsr.CB()\n")
		fmt.Fprintf(g, "res := rd - rs\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(res>rd)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetVSub(rd, rs, res)\n")
		fmt.Fprintf(g, "res += cf-1\n")
	case 7: // ROR
		fmt.Fprintf(g, "rot := (rs&0xFF)\n")
		fmt.Fprintf(g, "if rot != 0 { cpu.Cpsr.SetC(rd & (1<<(rot-1)) != 0) }\n")
		fmt.Fprintf(g, "rot = (rs&0x1F)\n")
		fmt.Fprintf(g, "res := (rd >> rot) | (rd << (32-rot))\n")
	case 9: // NEG
		fmt.Fprintf(g, "res := 0 - rs\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(true)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetVSub(0, rs, res)\n")
	case 10: // CMP
		test = true
		fmt.Fprintf(g, "res := rd - rs\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(res>rd)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetVSub(rd, rs, res)\n")
	case 11: // CMN
		test = true
		fmt.Fprintf(g, "res := rd + rs\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetC(res<rd)\n")
		fmt.Fprintf(g, "cpu.Cpsr.SetVAdd(rd, rs, res)\n")
	case 12: // ORR
		fmt.Fprintf(g, "res := rd | rs\n")
	case 13: // MUL
		fmt.Fprintf(g, "res := rd * rs\n")
	case 14: // BIC
		fmt.Fprintf(g, "res := rd &^ rs\n")
	case 15: // MVN
		fmt.Fprintf(g, "res := ^rs\n")
	default:
		panic("unreachable")
	}

	fmt.Fprintf(g, "cpu.Cpsr.SetNZ(res)\n")
	if !test {
		fmt.Fprintf(g, "cpu.Regs[rdx] = reg(res)\n")
	}
	g.writeOpAluFooter(op)
}

func (g *Generator) WriteOp(op uint16) {
	g.writeOpHeader(op)

	oph := op >> 8
	switch {
	case oph>>5 == 0x0 && (oph>>3)&3 != 3: // F1
		g.writeOpF1Shift(op)

	case oph>>5 == 0x0 && (oph>>3)&3 == 3: // F2
		g.writeOpF2Add(op)

	case oph>>5 == 0x1: // F3
		g.writeOpF3AluImm(op)

	case oph>>2 == 0x10: // F4
		g.writeOpF4Alu(op)

	case oph>>2 == 0x11: // F5
		g.writeOpF5HiReg(op)

	case oph>>3 == 0x9: // F6
		g.writeOpF6LdrPc(op)

	case oph>>4 == 0x5: // F7 & F8
		g.writeOpF7F8LdrStr(op)

	case oph>>5 == 0x3: // F9
		g.writeOpF9Strb(op)

	case oph>>4 == 0x8: // F10
		g.writeOpF10Strh(op)

	case oph>>4 == 0xB && oph&6 == 4: // F14
		g.writeOpF14PushPop(op)

	case oph>>4 == 0xD: // F16
		g.writeOpF16BranchCond(op)

	case oph>>3 == 0x1E: // F19
		g.writeOpF19LongBranch1(op)
	case oph>>3 == 0x1F || oph>>3 == 0x1D: // F19
		g.writeOpF19LongBranch2(op)

	default:
		g.writeOpInvalid(op, "not implemented")
	}

	g.writeOpFooter(op)
}

func main() {
	flag.Parse()

	var f io.Writer
	if *filename == "-" {
		f = os.Stdout
	} else {
		ff, err := os.Create(*filename)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		defer func() {
			cmd := exec.Command("go", "fmt", *filename)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				os.Exit(1)
			}
		}()
		defer ff.Close()
		f = ff
	}

	out := Generator{Writer: f}
	out.WriteHeader()
	for op := 0; op < 0x100; op++ {
		out.WriteOp(uint16(op << 8))
	}
	for op := 0; op < 0x10; op++ {
		out.WriteAluOp(uint16(op<<6) | uint16(0x10<<10))
	}

	out.WriteFooter()
}
