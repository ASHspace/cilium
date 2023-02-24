// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package bpf

import (
	"encoding/binary"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"

	"github.com/cilium/cilium/pkg/testutils"
)

// Generate a program of sufficient size whose verifier log does not fit into a
// 128-byte buffer. Load the program while requesting a verifier log using an
// undersized buffer and expect the load to be successful.
func TestLoadCollectionResizeLogBuffer(t *testing.T) {
	testutils.PrivilegedTest(t)

	num := 32
	insns := make(asm.Instructions, 0, num)
	for i := 0; i < num-1; i++ {
		insns = append(insns, asm.Mov.Reg(asm.R0, asm.R1))
	}
	insns = append(insns, asm.Return())

	spec := &ebpf.CollectionSpec{
		Programs: map[string]*ebpf.ProgramSpec{
			"test": {
				Type:         ebpf.SocketFilter,
				License:      "MIT",
				Instructions: insns,
			},
		},
	}

	coll, err := LoadCollection(spec, ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			// Request instruction-level verifier state to ensure sufficient
			// output is generated by the verifier. For example, one instruction:
			// 0: (bf) r0 = r1		; R0_w=ctx(off=0,imm=0) R1=ctx(off=0,imm=0)
			LogLevel: ebpf.LogLevelInstruction,
			// Set the minimum buffer size the kernel will accept. LoadCollection is
			// expected to grow this sufficiently over multiple tries.
			LogSize: 128,
		},
	})
	if err != nil {
		t.Fatal("Error loading collection:", err)
	}

	// Expect successful program creation, with a complementary verifier log.
	log := coll.Programs["test"].VerifierLog
	if len(log) == 0 {
		t.Fatal("Received empty verifier log")
	}
}

func TestInlineGlobalData(t *testing.T) {
	spec := &ebpf.CollectionSpec{
		ByteOrder: binary.LittleEndian,
		Maps: map[string]*ebpf.MapSpec{
			globalDataMap: {
				Contents: []ebpf.MapKV{{Value: []byte{
					0x0, 0x0, 0x0, 0x80,
					0x1, 0x0, 0x0, 0x0,
				}}},
			},
		},
		Programs: map[string]*ebpf.ProgramSpec{
			"prog1": {
				Instructions: asm.Instructions{
					// Pseudo-load at offset 0. This Instruction would have func_info when
					// read from an ELF, so validate Metadata preservation after inlining
					// global data.
					asm.LoadMapValue(asm.R0, 0, 0).WithReference(globalDataMap).WithSymbol("func1"),
					// Pseudo-load at offset 4.
					asm.LoadMapValue(asm.R0, 0, 4).WithReference(globalDataMap),
					asm.Return(),
				},
			},
		},
	}

	if err := inlineGlobalData(spec); err != nil {
		t.Fatal(err)
	}

	ins := spec.Programs["prog1"].Instructions[0]
	if want, got := 0x80000000, int(ins.Constant); want != got {
		t.Errorf("unexpected Instruction constant: want: 0x%x, got: 0x%x", want, got)
	}

	if want, got := "func1", ins.Symbol(); want != got {
		t.Errorf("unexpected Symbol value of Instruction: want: %s, got: %s", want, got)
	}

	ins = spec.Programs["prog1"].Instructions[1]
	if want, got := 0x1, int(ins.Constant); want != got {
		t.Errorf("unexpected Instruction constant: want: 0x%x, got: 0x%x", want, got)
	}
}