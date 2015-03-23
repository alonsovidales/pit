package instances

import (
	"os"
	"testing"
	"time"
)

var im *InstancesModel

func TestMain(m *testing.M) {
	im = InitAndKeepAlive("test", "eu-west-1", true)
	time.Sleep(time.Second)

	retCode := m.Run()

	im.delTable()

	os.Exit(retCode)
}

func TestAddGetNoInstance(t *testing.T) {
	if len(im.GetInstances()) != 0 {
		t.Error("The test shouldn't return any instance, but:", im.GetInstances(), "was returned")
	}
}

func TestAddGetInstances(t *testing.T) {
	im.registerHostName("test1")
	im.registerHostName("test2")
	im.registerHostName("test3")
	time.Sleep(time.Second)
	if len(im.GetInstances()) != 3 {
		t.Error("The test should to return 3 instances, but:", im.GetInstances(), "was returned")
	}

	time.Sleep(cTTL * 2 * time.Second)
	if len(im.GetInstances()) != 0 {
		t.Error("The test should to return 0 instances, but:", im.GetInstances(), "was returned")
	}
}
