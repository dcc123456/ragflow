//go:build cgo && ee

package native

import "testing"

func TestParseDevice(t *testing.T) {
	cases := []struct {
		spec    string
		wantGPU bool
		wantID  int
		wantErr bool
	}{
		{"", false, 0, false},
		{"cpu", false, 0, false},
		{" CPU ", false, 0, false},
		{"gpu", true, 0, false},
		{"GPU", true, 0, false},
		{"gpu:0", true, 0, false},
		{"gpu:1", true, 1, false},
		{"gpu: 2 ", true, 2, false},
		{"gpu:-1", false, 0, true},
		{"gpu:abc", false, 0, true},
		{"tpu", false, 0, true},
		{"gpu:1:2", false, 0, true},
	}
	for _, tc := range cases {
		got, err := ParseDevice(tc.spec)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseDevice(%q) error=%v, wantErr=%v", tc.spec, err, tc.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if got.UseGPU != tc.wantGPU || got.DeviceID != tc.wantID {
			t.Errorf("ParseDevice(%q) = {gpu:%v id:%d}, want {gpu:%v id:%d}", tc.spec, got.UseGPU, got.DeviceID, tc.wantGPU, tc.wantID)
		}
	}
}

func TestDeviceConfigString(t *testing.T) {
	if got := (DeviceConfig{}).String(); got != "cpu" {
		t.Errorf("cpu String() = %q, want cpu", got)
	}
	if got := (DeviceConfig{UseGPU: true}).String(); got != "gpu:0" {
		t.Errorf("gpu String() = %q, want gpu:0", got)
	}
	if got := (DeviceConfig{UseGPU: true, DeviceID: 2}).String(); got != "gpu:2" {
		t.Errorf("gpu:2 String() = %q, want gpu:2", got)
	}
}

func TestSetGetDevice(t *testing.T) {
	orig := getDevice()
	defer SetDevice(orig)

	SetDevice(DeviceConfig{UseGPU: true, DeviceID: 1, MemLimitMB: 512})
	got := getDevice()
	if !got.UseGPU || got.DeviceID != 1 || got.MemLimitMB != 512 {
		t.Fatalf("getDevice() = %+v", got)
	}
	// clamping
	SetDevice(DeviceConfig{UseGPU: false, DeviceID: -5, MemLimitMB: -1})
	got = getDevice()
	if got.DeviceID != 0 || got.MemLimitMB != 0 {
		t.Fatalf("clamp failed: %+v", got)
	}
}
