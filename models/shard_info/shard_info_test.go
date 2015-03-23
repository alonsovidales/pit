package shardinfo

import (
	//"github.com/alonsovidales/pit/log"
	//"github.com/alonsovidales/pit/models/instances"
	"os"
	//"reflect"
	"testing"
	"time"
)

var md *Model

func TestMain(m *testing.M) {
	md = GetModel("test", "eu-west-1")
	time.Sleep(time.Second * 20)

	retCode := m.Run()

	//md.delTables()

	os.Exit(retCode)
}

func TestAddGroupPersistAndRead(t *testing.T) {
	var err error

	originalGroups := make([]*GroupInfo, 3)
	originalGroups[0], err = md.AddGroup("userId", "secret", "groupId", 3, 1000000, 100, 1000, 5)
	if err != nil {
		t.Error("Problem trying to insert a new group, Error:", err)
		t.Fail()
	}

	time.Sleep(5 * time.Second)
}
