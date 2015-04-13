package users

import (
	"os"
	"reflect"
	"testing"
	"time"
)

var um *UsersModel

func TestMain(m *testing.M) {
	um = GetModel("test", "eu-west-1")
	time.Sleep(time.Second)

	retCode := m.Run()

	//um.delTable()

	os.Exit(retCode)
}

func TestAddGetInstances(t *testing.T) {
	user := um.RegisterUser("uid", "key", "127.0.0.1")
	if user == nil {
		t.Error("A new user can't be registered")
	}

	if user.uid != "uid" || user.key != "key" {
		t.Error("The user information stored doesn't corresponds with the returned, User:", user)
	}

	time.Sleep(time.Second)

	secUser := um.GetUserInfo("uid", "key")

	if !reflect.DeepEqual(user, secUser) {
		t.Error("The returned used is not equal to the inserted", user, secUser)
	}

	secUser.DisableUser()
	time.Sleep(time.Second)
	secUser = um.GetUserInfo("uid", "key")
	if secUser != nil {
		t.Error("The user wasn't disabled")
	}
}
