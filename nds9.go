package main

import (
	"dsemu/arm"
	"io/ioutil"
	"unsafe"

	log "gopkg.in/Sirupsen/logrus.v0"
)

type NDS9 struct {
	Cpu *arm.Cpu
	Bus *BankedBus

	Ram [4 * 1024 * 1024]byte
}

func NewNDS9() *NDS9 {
	bios9, err := ioutil.ReadFile("bios/biosnds9.rom")
	if err != nil {
		log.Fatal(err)
	}

	bus := BankedBus{}

	cpu := arm.NewCpu(&bus)
	cpu.EnableCp15()

	nds9 := &NDS9{
		Cpu: cpu,
		Bus: &bus,
	}

	bus.MapMemory(0x02000000, unsafe.Pointer(&nds9.Ram[0]), len(nds9.Ram), false)
	bus.MapMemory(0x02400000, unsafe.Pointer(&nds9.Ram[0]), len(nds9.Ram), false)
	bus.MapMemory(0x02800000, unsafe.Pointer(&nds9.Ram[0]), len(nds9.Ram), false)
	bus.MapMemory(0x02C00000, unsafe.Pointer(&nds9.Ram[0]), len(nds9.Ram), false)
	bus.MapMemory(0x0FFF0000, unsafe.Pointer(&bios9[0]), len(bios9), true)

	return nds9
}
