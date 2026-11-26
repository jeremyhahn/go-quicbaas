package orchestration

import (
	"errors"
	"reflect"
	"testing"
)

func brokerSpec() *ContainerSpec {
	return &ContainerSpec{
		Name: "nbd", Image: "registry.example.com/nbd:1", PrivilegeClass: PrivilegeClassBroker,
		Devices: []ContainerDevice{{HostPath: "/dev/nbd0", ContainerPath: "/dev/vol/root", Permissions: "rw"}},
	}
}

// Devices are the broker class's alone: the same spec in the ordinary class
// is refused, and an unknown class is refused outright.
func TestContainerSpecDevicesNeedBrokerClass(t *testing.T) {
	spec := brokerSpec()
	if err := spec.Validate(); err != nil {
		t.Fatalf("broker spec: Validate = %v", err)
	}
	spec.PrivilegeClass = ""
	if err := spec.Validate(); !errors.Is(err, ErrDevicePassthroughDenied) {
		t.Errorf("ordinary class with devices: Validate = %v, want ErrDevicePassthroughDenied", err)
	}
	spec.PrivilegeClass = "root"
	var iv *InputValidationError
	if err := spec.Validate(); !errors.As(err, &iv) || iv.Field != "privilege_class" {
		t.Errorf("unknown class: Validate = %v", err)
	}
}

func TestContainerDeviceValidate(t *testing.T) {
	for _, d := range []ContainerDevice{
		{HostPath: "dev/nbd0"},
		{HostPath: "/dev/../etc/shadow"},
		{HostPath: "/dev/nbd0", ContainerPath: "/a:b"},
		{HostPath: "/dev/nbd0", Permissions: "rx"},
		{HostPath: "/dev/nbd0", Permissions: "rr"},
	} {
		if err := d.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want an error", d)
		}
	}
	spec := brokerSpec()
	spec.Devices[0].HostPath = "nbd0"
	var iv *InputValidationError
	if err := spec.Validate(); !errors.As(err, &iv) || iv.Field != "devices[0]" {
		t.Errorf("bad device in spec: Validate = %v", err)
	}
}

func TestContainerDeviceRunArgs(t *testing.T) {
	spec := brokerSpec()
	spec.Devices = append(spec.Devices, ContainerDevice{HostPath: "/dev/nbd1"}, ContainerDevice{HostPath: "/dev/nbd2", Permissions: "r"})
	want := []string{"--device", "/dev/nbd0:/dev/vol/root:rw", "--device", "/dev/nbd1", "--device", "/dev/nbd2:/dev/nbd2:r"}
	if got := spec.PrivilegeRunArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("PrivilegeRunArgs = %q, want %q", got, want)
	}
}

// The class and the devices are measured: a broker spec does not measure as
// the same spec without its devices or its class.
func TestContainerConfigDigestMeasuresDevices(t *testing.T) {
	digest := func(s *ContainerSpec) string {
		d, err := ContainerConfigDigest(s)
		if err != nil {
			t.Fatalf("ContainerConfigDigest: %v", err)
		}
		return d
	}
	broker := brokerSpec()
	noDevices := brokerSpec()
	noDevices.Devices = nil
	otherDevice := brokerSpec()
	otherDevice.Devices[0].HostPath = "/dev/nbd9"
	plain := brokerSpec()
	plain.Devices, plain.PrivilegeClass = nil, ""

	seen := map[string]string{}
	for name, s := range map[string]*ContainerSpec{"broker": broker, "no devices": noDevices, "other device": otherDevice, "plain": plain} {
		d := digest(s)
		if prev, dup := seen[d]; dup {
			t.Errorf("%s and %s measure the same", name, prev)
		}
		seen[d] = name
	}
}
