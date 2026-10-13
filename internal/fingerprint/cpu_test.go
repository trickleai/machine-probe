package fingerprint

import "testing"

func TestVirtioMmioDevice(t *testing.T) {
	for name, want := range map[string]bool{
		"virtio-mmio.0":          true, // kernel command line
		"40003000.virtio_mmio":   true, // device tree (Firecracker arm64)
		"LNRO0005:00":            true, // ACPI (Firecracker x86)
		"40001000.rtc":           false,
		"serial8250":             false,
		"AMZNC10C:00":            false,
		"virtio_mmio_controller": false,
	} {
		if got := virtioMmioDevice(name); got != want {
			t.Errorf("virtioMmioDevice(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestGuestSignals(t *testing.T) {
	// Values seen inside a Firecracker microVM on a Graviton3 host.
	ev, vendor := guestSignals([]string{"KVM virtual PTP"}, "linux,dummy-virt\x00", "",
		"[    0.000000] smccc: KVM: hypervisor services detected (0x00000000 0x00000000 0x00000000 0x00000003)\n")
	if vendor != "KVM" || len(ev) != 3 {
		t.Fatalf("arm64 microVM: vendor=%q ev=%q", vendor, ev)
	}

	ev, vendor = guestSignals(nil, "", "xen,xen-4.17\x00xen,xen\x00", "")
	if vendor != "Xen" || len(ev) != 1 {
		t.Fatalf("Xen guest: vendor=%q ev=%q", vendor, ev)
	}

	// A bare-metal host with a NIC PTP clock and a vendor board.
	ev, vendor = guestSignals([]string{"ena-ptp"}, "amazon,graviton\x00", "", "psci: PSCIv1.1 detected in firmware.\n")
	if vendor != "" || len(ev) != 0 {
		t.Fatalf("bare metal: vendor=%q ev=%q", vendor, ev)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name                string
		gvisor, vm, micro   bool
		signals             int
		wantClass, wantText string
	}{
		{"container in arm64 microVM", false, true, true, 1, "container-in-vm", "Container inside a microVM (KVM)"},
		{"microVM", false, true, true, 0, "microvm", "microVM (KVM)"},
		{"VM", false, true, false, 0, "vm", "Virtual machine (KVM)"},
		{"container", false, false, true, 1, "container", "Container (shared host kernel)"},
		{"gVisor", true, true, true, 1, "gvisor", "gVisor (user-space kernel)"},
		{"bare metal", false, false, false, 0, "bare-metal-or-unknown", "Bare metal or undetectable"},
	}
	for _, c := range cases {
		class, label := classify(c.gvisor, c.vm, c.micro, c.signals, "KVM", "KVM")
		if class != c.wantClass || label != c.wantText {
			t.Errorf("%s: got %s / %q", c.name, class, label)
		}
	}
}
