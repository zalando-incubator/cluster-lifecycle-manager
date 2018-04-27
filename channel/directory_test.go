package channel

import "testing"

func TestDirectoryChannel(t *testing.T) {
	location := "/test-dir"
	channel := "local"

	d := NewDirectory(location)
	err := d.Update()
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	cc, err := d.Get(channel)
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	if cc.Path != location {
		t.Errorf("expected %s, got %s", location, cc.Path)
	}

	if cc.Version != channel {
		t.Errorf("expected %s, got %s", channel, cc.Version)
	}
}
