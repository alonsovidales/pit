package shardinfo

import (
	"os"
	"testing"
	"time"
)

var md *Model

func TestMain(m *testing.M) {
	md = GetModel("test", "eu-west-1")
	time.Sleep(time.Second * 20)

	retCode := m.Run()

	md.delTable()

	os.Exit(retCode)
}

func TestAddGetNoInstance(t *testing.T) {
}
