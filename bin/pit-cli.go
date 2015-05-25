package main

import (
	"fmt"
	"github.com/alonsovidales/pit/cfg"
	"github.com/alonsovidales/pit/models/instances"
	"github.com/alonsovidales/pit/models/shard_info"
	"github.com/alonsovidales/pit/models/users"
	"gopkg.in/alecthomas/kingpin.v1"
	"os"
	"strings"
	"text/tabwriter"
)

// Terminal text colours
const (
	// CLR0 Default terminal colour
	CLR0 = "\x1b[30;1m"
	// CLRR Red colour
	CLRR = "\x1b[31;1m"
	// CLRG Green colour
	CLRG = "\x1b[32;1m"
	// CLRY Yellow colour
	CLRY = "\x1b[33;1m"
	// CLRB Blue colour
	CLRB = "\x1b[34;1m"
	// CLRN Neutral colour
	CLRN = "\x1b[0m"
)

func main() {
	app := kingpin.New("pit-cli", "Pit-cli is a tool to manage all the different features of the recommener system Pit")

	env := app.Flag("env", "Configuration environment: pro, pre, dev").Required().Enum("pro", "pre", "dev")

	cmdInstances := app.Command("instances", "Manage all the instances of the cluster")
	cmdInstancesList := cmdInstances.Command("list", "Lists all the instances on the clusted")

	cmdUsers := app.Command("users", "Users management")
	cmdUsersList := cmdUsers.Command("list", "lists all the users")

	cmdUsersAdd := cmdUsers.Command("add", "Adds a new user")
	cmdUsersAddUID := cmdUsersAdd.Arg("user-ID", "User ID").Required().String()
	cmdUsersAddKey := cmdUsersAdd.Arg("key", "User Password").Required().String()

	cmdUsersShow := cmdUsers.Command("show", "Shows all the sotred information for a specific user")
	cmdUsersShowUID := cmdUsersShow.Arg("user-ID", "User ID").Required().String()
	cmdUsersEnable := cmdUsers.Command("enable", "Enables a disabled user account")
	cmdUsersEnableUID := cmdUsersEnable.Arg("user-ID", "User ID").Required().String()
	cmdUsersDisable := cmdUsers.Command("disable", "Disables an enabled user account")
	cmdUsersDisableUID := cmdUsersDisable.Arg("user-ID", "User ID").Required().String()

	cmdGroups := app.Command("groups", "Manage all the recommendation groups allocated by Pit")
	cmdGroupsList := cmdGroups.Command("list", "lists all the shards in the system")
	cmdGroupsListUser := cmdGroupsList.Flag("userid", `List the instances for this user`).String()

	cmdGroupsDel := cmdGroups.Command("del", "Removes one of the groups")
	cmdGroupsDelGroupID := cmdGroupsDel.Arg("group-id", `ID of the group to be removed`).Required().String()

	cmdGroupsAdd := cmdGroups.Command("update", "Adds or updates an existing shard")
	cmdGroupsAddMaxScore := cmdGroupsAdd.Flag("max-score", `Max possible score`).Required().Int()
	cmdGroupsAddNumShards := cmdGroupsAdd.Flag("num-shards", `Total number of shards`).Required().Int()
	cmdGroupsAddMaxElements := cmdGroupsAdd.Flag("num-elems", `Max number of elements that can be allocated by shard`).Required().Int()
	cmdGroupsAddMaxReqSec := cmdGroupsAdd.Flag("max-req-sec", `Max number of requests by second`).Required().Int()
	cmdGroupsAddMaxInsertReqSec := cmdGroupsAdd.Flag("max-ins-req-sec", `Max number of insert requests`).Required().Int()
	cmdGroupsAddUserID := cmdGroupsAdd.Flag("user-id", `User ID of the owner of this group`).Required().String()
	cmdGroupsAddGroupID := cmdGroupsAdd.Flag("group-id", `ID of the group to be updated or added`).Required().String()

	kingpin.MustParse(app.Parse(os.Args[1:]))
	cfg.Init("pit", *env)
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case cmdInstancesList.FullCommand():
		listInstances()

	case cmdGroupsList.FullCommand():
		listGroups(*cmdGroupsListUser)

	case cmdGroupsDel.FullCommand():
		delGroup(*cmdGroupsDelGroupID)

	case cmdGroupsAdd.FullCommand():
		addGroup(
			*cmdGroupsAddUserID,
			*cmdGroupsAddGroupID,
			*cmdGroupsAddNumShards,
			uint64(*cmdGroupsAddMaxElements),
			uint64(*cmdGroupsAddMaxReqSec),
			uint64(*cmdGroupsAddMaxInsertReqSec),
			uint8(*cmdGroupsAddMaxScore))

	case cmdUsersAdd.FullCommand():
		addUser(*cmdUsersAddUID, *cmdUsersAddKey)

	case cmdUsersList.FullCommand():
		listUsers()

	case cmdUsersShow.FullCommand():
		showUserInfo(*cmdUsersShowUID)

	case cmdUsersEnable.FullCommand():
		enableUser(*cmdUsersEnableUID)

	case cmdUsersDisable.FullCommand():
		disableUser(*cmdUsersDisableUID)

	default:
		fmt.Printf("Not command specified, use: \"%s --help\" to get help\n", strings.Join(os.Args, " "))
	}
}

func listUsers() {
	md := users.GetModel(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"))

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 8, 3, '\t', 0)
	fmt.Fprintln(w, "Uid\tEnabled\tRegTs\tRegIP\tLogLines")
	fmt.Fprintln(w, "---\t-------\t-----\t-----\t--------")

	for uid, user := range md.GetRegisteredUsers() {
		lines := 0
		for _, v := range user.GetAllActivity() {
			lines += len(v)
		}
		fmt.Fprintf(
			w,
			"%s\t%s\t%d\t%s\t%d\n",
			uid,
			user.Enabled,
			user.RegTs,
			user.RegIP,
			lines)
	}
	w.Flush()
}

func addUser(cmdUsersAddUID string, cmdUsersAddKey string) {
	md := users.GetModel(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"))

	if _, err := md.RegisterUser(cmdUsersAddUID, cmdUsersAddKey, "127.0.0.1"); err != nil {
		fmt.Println("Problem trying to register the user:", err)
	} else {
		fmt.Println("User registered")
	}
}

func showUserInfo(uid string) {
	md := users.GetModel(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"))

	user := md.AdminGetUserInfoByID(uid)
	if user == nil {
		fmt.Println("User Not Found")
		return
	}

	fmt.Println("User info:")
	fmt.Println("ID:", uid)
	fmt.Println("Enabled:", user.Enabled)
	fmt.Println("Registered TS:", user.RegTs)
	fmt.Println("Registered IP:", user.RegIP)
	fmt.Println("Activity Logs:")

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 8, 3, '\t', 0)
	fmt.Fprintln(w, "Timestamp\tType\tDescripton")
	fmt.Fprintln(w, "---------\t----\t----------")

	for _, lines := range user.GetAllActivity() {
		for _, line := range lines {
			fmt.Fprintf(
				w,
				"%d\t%s\t%s\n",
				line.Ts,
				line.LogType,
				line.Desc)
		}
	}
	w.Flush()
}

func disableUser(uid string) {
	fmt.Println("The user with user ID:", uid, "will be disabled")
	if askForConfirmation() {
		md := users.GetModel(
			cfg.GetStr("aws", "prefix"),
			cfg.GetStr("aws", "region"))

		user := md.AdminGetUserInfoByID(uid)
		if user != nil {
			user.DisableUser()
			fmt.Println("User Disabled")
		} else {
			fmt.Println("User not found")
		}
	}
}

func enableUser(uid string) {
	fmt.Println("The user with user ID:", uid, "will be enabled")
	if askForConfirmation() {
		md := users.GetModel(
			cfg.GetStr("aws", "prefix"),
			cfg.GetStr("aws", "region"))

		user := md.AdminGetUserInfoByID(uid)
		if user != nil {
			user.EnableUser()
			fmt.Println("User Enabled")
		} else {
			fmt.Println("User not found")
		}
	}
}

func listInstances() {
	md := instances.InitAndKeepAlive(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"),
		false)

	fmt.Println(CLRG + "Instances" + CLRN)
	fmt.Println(CLRG + "---------" + CLRN)
	for _, instance := range md.GetInstances() {
		fmt.Println(instance)
	}
}

func listGroups(userID string) {
	md := shardinfo.GetModel(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"),
		"")

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 8, 3, '\t', 0)
	fmt.Fprintln(w, "User ID\tSecret\tGroupID\tMax Score\tTotal Shards\tMax Elements\tMax req sec\tMax Insert Req Sec\tShard owners")
	fmt.Fprintln(w, "-------\t------\t-------\t---------\t------------\t------------\t-----------\t------------------\t------------")

	groups := md.GetAllGroups()
	for _, groups := range groups {
		for _, group := range groups {
			shardOwners := ""
			for _, shard := range group.Shards {
				shardOwners += fmt.Sprintf("%s %d\t", shard.Addr, shard.LastTs)
			}

			fmt.Fprintf(
				w,
				"%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
				group.UserID,
				group.Secret,
				group.GroupID,
				group.MaxScore,
				group.NumShards,
				group.MaxElements,
				group.MaxReqSec,
				group.MaxInsertReqSec,
				shardOwners)
		}
	}
	w.Flush()
}

func delGroup(groupID string) {
	fmt.Println("The next group will be deleted:")
	md := shardinfo.GetModel(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"),
		"")

	group := md.GetGroupByID(groupID)
	if group == nil {
		fmt.Println("Group not found with ID:", groupID)
		return
	}

	if askForConfirmation() {
		md.RemoveGroup(groupID)
		fmt.Println("Group removed")
	}
}

func addGroup(userID, groupID string, numShards int, maxElements, maxReqSec, maxInsertReqSec uint64, maxScore uint8) {
	md := shardinfo.GetModel(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"),
		"")

	fmt.Println(CLRG + "The next group will be added:" + CLRN)
	fmt.Println("User ID:", userID)
	fmt.Println("Group ID:", groupID)
	fmt.Println("Num Shards:", numShards)
	fmt.Println("Max elements:", maxElements)
	fmt.Println("Max requests by sec / shard:", maxReqSec)
	fmt.Println("Max Insert requests by sec / shard:", maxInsertReqSec)
	fmt.Println("Max score:", maxScore)

	if askForConfirmation() {
		_, key, err := md.AddUpdateGroup("Custom", userID, groupID, numShards, maxElements, maxReqSec, maxInsertReqSec, maxScore)
		if err != nil {
			fmt.Println("Problem adding a new group, Error:", err)
		} else {
			fmt.Println("Group added, key:", key)
		}
	}
}

func askForConfirmation() bool {
	var response string

	fmt.Print(CLRR + "\nAre you completly sure? (y/n): " + CLRN)
	if _, err := fmt.Scanln(&response); err != nil {
		return false
	}

	possibleAnsw := map[string]bool{
		"y":   true,
		"Y":   true,
		"yes": true,
		"Yes": true,
		"YES": true,
	}

	_, ok := possibleAnsw[response]

	return ok
}
