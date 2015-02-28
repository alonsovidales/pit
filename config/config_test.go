package config

import (
	"testing"
)

func TestDebugLevel(t *testing.T) {
	if err := Init("config", "dev"); err != nil {
		t.Error("Test config file can't be loaded")
		t.Fail()
	}

	if GetStr("section1", "val_str") != "test" {
		t.Error("Expected value for section \"section1\" and \"val_str\" filed was \"test\"")
	}

	if GetInt("section1", "val_int") != 123 {
		t.Error("Expected value for section \"section1\" and \"val_int\" filed was \"123\"")
	}

	if !GetBool("section1", "val_bool_true") {
		t.Error("Expected value for section \"section1\" and \"val_bool_true\" filed was \"true\"")
	}

	if GetBool("section1", "val_bool_false") {
		t.Error("Expected value for section \"section1\" and \"val_bool_false\" filed was \"false\"")
	}

	if !GetBool("section2", "val_bool_true") {
		t.Error("Expected value for section \"section2\" and \"val_bool_true\" filed was \"true\"")
	}

	if GetBool("section2", "val_bool_false") {
		t.Error("Expected value for section \"section2\" and \"val_bool_false\" filed was \"false\"")
	}
}
