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

	um.delTable()

	os.Exit(retCode)
}

func TestAddGetListUsers(t *testing.T) {
	user, err := um.RegisterUser("uid", "key", "127.0.0.1")
	if user == nil || err != nil {
		t.Error("A new user can't be registered")
	}
	time.Sleep(time.Second)
	if userAux, err := um.RegisterUser("uid", "key", "127.0.0.1"); err == nil || userAux != nil {
		t.Error("Duplicated user registration")
	}

	if user.uid != "uid" {
		t.Error("The user information stored doesn't corresponds with the returned, User:", user)
	}

	time.Sleep(time.Second)

	u1, err := um.RegisterUser("uid1", "key1", "127.0.0.2")
	if err != nil {
		t.Error("A new user can't be registered")
	}
	u2, err := um.RegisterUser("uid2", "key2", "127.0.0.3")
	if err != nil {
		t.Error("A new user can't be registered")
	}
	usersToBeReturned := map[string]*User{
		"uid":  user,
		"uid1": u1,
		"uid2": u2,
	}

	usersRegistered := um.GetRegisteredUsers()
	for k, v := range usersToBeReturned {
		if !reflect.DeepEqual(usersRegistered[k], v) {
			t.Error("The returned registered users are not equal to the inserted", usersRegistered[k], v)
		}
	}

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

func TestUpdateUser(t *testing.T) {
	user, err := um.RegisterUser("uid99", "key99", "127.0.0.1")
	if user == nil || err != nil {
		t.Error("A new user can't be registered")
	}
	user.AddActivityLog("test", "testing...")

	userFromDb := um.GetUserInfo("uid99", "key99")

	if _, ok := userFromDb.logs["test"]; !ok || !reflect.DeepEqual(userFromDb.logs, user.logs) {
		t.Error("The inserted logs doesn't match with the returned from DB:", userFromDb.logs, user.logs)
	}
}
