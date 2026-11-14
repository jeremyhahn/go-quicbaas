package orchestration_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	pkgorch "github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	sdkorch "github.com/jeremyhahn/go-quicbaas/sdk/go/orchestration"
)

// TestVolumeTypesCrossBoundary: an attached volume built through the SDK is
// the server's value, and its JSON decodes into the server's type unchanged.
// This is the seam go-trusted-platform's NBD export consumes.
func TestVolumeTypesCrossBoundary(t *testing.T) {
	sdkVol := sdkorch.AttachedVolume{
		Claim: sdkorch.VolumeClaim{
			Name:      "root-node7",
			SizeBytes: 8 << 30,
			FSType:    "ext4",
			Target:    "/var/lib/data",
			ReadOnly:  true,
			Replicas:  2,
			Labels:    map[string]string{"boot": "diskless"},
		},
		VolumeID:   "root-node7",
		DevicePath: "/dev/mapper/root-node7",
		HostPath:   "/run/volumes/root-node7",
	}
	var pkgVol pkgorch.AttachedVolume = sdkVol // no conversion
	raw, err := json.Marshal(sdkVol)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back pkgorch.AttachedVolume
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back, pkgVol) {
		t.Errorf("round trip = %+v, want %+v", back, pkgVol)
	}

	mounts := sdkorch.ContainerMounts([]sdkorch.AttachedVolume{sdkVol, {Claim: sdkorch.VolumeClaim{Name: "raw"}, VolumeID: "raw", DevicePath: "/dev/nbd0"}})
	want := []sdkorch.ContainerMount{{Source: "/run/volumes/root-node7", Target: "/var/lib/data", ReadOnly: true, Type: sdkorch.MountTypeBind}}
	if !reflect.DeepEqual(mounts, want) {
		t.Errorf("ContainerMounts = %+v, want %+v (raw volume has no mount)", mounts, want)
	}
}

// TestVolumeErrorsCrossBoundary: the SDK's sentinels are the server's.
// A broker-class SDK spec carries devices through to the server's Validate;
// the ordinary class is refused with the same sentinel on both sides.
func TestDevicePassthroughCrossBoundary(t *testing.T) {
	devs := sdkorch.ContainerDevices([]sdkorch.AttachedVolume{
		{Claim: sdkorch.VolumeClaim{Name: "root", Target: "/dev/vol/root"}, DevicePath: "/dev/nbd0"},
	})
	want := []sdkorch.ContainerDevice{{HostPath: "/dev/nbd0", ContainerPath: "/dev/vol/root", Permissions: "rw"}}
	if !reflect.DeepEqual(devs, want) {
		t.Fatalf("ContainerDevices = %+v, want %+v", devs, want)
	}
	spec := &sdkorch.ContainerSpec{Image: "registry.example.com/nbd:1", PrivilegeClass: sdkorch.PrivilegeClassBroker, Devices: devs}
	if err := spec.Validate(); err != nil {
		t.Fatalf("broker spec: Validate = %v", err)
	}
	spec.PrivilegeClass = ""
	if err := spec.Validate(); !errors.Is(err, sdkorch.ErrDevicePassthroughDenied) || !errors.Is(err, pkgorch.ErrDevicePassthroughDenied) {
		t.Errorf("ordinary class: Validate = %v, want ErrDevicePassthroughDenied", err)
	}
}

func TestVolumeErrorsCrossBoundary(t *testing.T) {
	err := sdkorch.VolumeClaim{Name: "data", SizeBytes: 0}.Validate()
	if !errors.Is(err, sdkorch.ErrVolumeClaim) || !errors.Is(err, pkgorch.ErrVolumeClaim) {
		t.Errorf("Validate(size 0) = %v, want ErrVolumeClaim on both sides", err)
	}
	if _, err := sdkorch.NewVolumeAttacher(nil, "node-1", "/run/volumes"); !errors.Is(err, sdkorch.ErrVolumeInvalidArgument) {
		t.Errorf("NewVolumeAttacher(nil) = %v, want ErrVolumeInvalidArgument", err)
	}
	if _, err := sdkorch.NewVolumeProvider("no-such-provider", nil); err == nil {
		t.Error("NewVolumeProvider(unknown) = nil error, want an error")
	}
	for _, pair := range [][2]error{
		{sdkorch.ErrVolumeNotFound, pkgorch.ErrVolumeNotFound},
		{sdkorch.ErrVolumeUnsupported, pkgorch.ErrVolumeUnsupported},
		{sdkorch.ErrVolumeShrink, pkgorch.ErrVolumeShrink},
	} {
		if pair[0] != pair[1] {
			t.Errorf("SDK sentinel %v is not the server's", pair[0])
		}
	}
}
