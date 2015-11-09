package accountsmanager

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/models/users"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// CBillingInfo Endpoint for the service that will provide the client
	// billing info
	CBillingInfo = "/billing_info"
	// CRegisterPath Endpoint to be used to register new system accounts
	CRegisterPath = "/account_register"
	// CVerifyPath Endpoint to be used to verify the identity of an account
	CVerifyPath = "/account_verify"
	// CLogsPath Endpoint that will return the logs for an account
	CLogsPath = "/account_logs"
	// CDisablePath Endpoint to be used to disable an account
	CDisablePath = "/disable_account"
	// CRecoverPassPath Endpoint to be used to recover for account password
	// recovery
	CRecoverPassPath = "/recover_pass"
	// CChangePass Endpoint to be used to change an account password
	CChangePass = "/change_pass"
	// CResetPass Endpoint to be used to reset the password for an account
	CResetPass = "/reset_pass"

	// cConfirmEmailTTL Time in seconds that a recovery password token is
	// going to be valid
	cConfirmEmailTTL = 24 * 3600
)

// Manager Strcuture that will be used to manage all the accounts in the system
type Manager struct {
	usersModel     users.ModelInt
	baseURL        string
	secret         string
	mailFromAddr   string
	mailServerAddr string
	mailServerPort int64
}

// Init Initialize a Manager struct with the corresponding parameters and
// returns it
func Init(baseURL, mailFromAddr, mailServerAddr string, mailServerPort int64, usersModel users.ModelInt) (mg *Manager) {
	mg = &Manager{
		baseURL:        baseURL,
		secret:         os.Getenv("PIT_SECRET"),
		mailServerAddr: mailServerAddr,
		mailFromAddr:   mailFromAddr,
		mailServerPort: mailServerPort,
		usersModel:     usersModel,
	}

	return
}

// BillingInfo Returns as JSON the billing info for the given account
func (mg *Manager) BillingInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	uid := r.FormValue("u")
	key := r.FormValue("k")

	if userInfo := mg.usersModel.GetUserInfo(uid, key); userInfo != nil {
		logs := userInfo.GetBillingInfo()
		logsJSON, err := json.Marshal(logs)
		if err != nil {
			w.WriteHeader(500)
			w.Write([]byte("User billing logs can't be converted to JSON"))
			return
		}

		w.WriteHeader(200)
		w.Write(logsJSON)
	} else {
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprint("Unauthorized")))
	}
}

// Register a new account on the system
func (mg *Manager) Register(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Sanitize e-mail addr removin all the + Chars in order to avoid fake
	// duplicated accounts
	uid := strings.Replace(r.FormValue("uid"), "+", "", -1)
	key := r.FormValue("key")

	if uid == "" || key == "" {
		w.WriteHeader(400)
		w.Write([]byte("The uid and key parameters are required"))
		return
	}

	if mg.usersModel.AdminGetUserInfoByID(uid) != nil {
		w.WriteHeader(422)
		w.Write([]byte("The email address you have entered is already registered"))
		return
	}

	ttl := time.Now().Unix() + cConfirmEmailTTL
	keyHash := mg.usersModel.HashPassword(key)

	v := url.Values{}
	v.Set("u", uid)
	v.Set("k", keyHash)
	v.Set("t", fmt.Sprintf("%d", ttl))
	v.Set("s", mg.getSignature(uid, keyHash, ttl))

	verifURL := fmt.Sprintf(
		"%s/%s?%s",
		mg.baseURL,
		CVerifyPath,
		v.Encode())

	emailSent := mg.SendEmail(
		uid,
		fmt.Sprintf(
			"Hello from Pitia!,\n\tPlease, click on the next link in order to verify you account: %s\n\nBest!,",
			verifURL),
		"Account verification from Pitia")

	if !emailSent {
		w.WriteHeader(500)
		w.Write([]byte("Problem trying to send the verification e-mail"))

		return
	}

	w.WriteHeader(200)
	w.Write([]byte("Verification e-mail sent, please check your e-mail!"))
}

// Verify This method will be called in order to verify the identity of an
// account, using the link from the e-mail provided during the registration
func (mg *Manager) Verify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	uid := r.FormValue("u")
	key := r.FormValue("k")
	ttl, err := strconv.ParseInt(r.FormValue("t"), 10, 64)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("The privided timestamp can't be parsed as integer"))
		return
	}
	sign := r.FormValue("s")

	if sign != mg.getSignature(uid, key, ttl) {
		w.WriteHeader(403)
		w.Write([]byte("Signature error"))
		return
	}

	if ttl < time.Now().Unix() {
		w.WriteHeader(403)
		w.Write([]byte("Verification e-mail expired, please register again your account"))
		return
	}

	if user, err := mg.usersModel.RegisterUserPlainKey(uid, key, r.RemoteAddr); err != nil {
		w.WriteHeader(500)
		w.Write([]byte(fmt.Sprint(err)))
	} else {
		user.AddActivityLog(users.CActivityAccountType, "Account verified", r.RemoteAddr)
		w.WriteHeader(200)
		w.Write([]byte("Account verified"))
	}
}

// Logs Returns the logs for a provided account
func (mg *Manager) Logs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	uid := r.FormValue("u")
	key := r.FormValue("k")

	if userInfo := mg.usersModel.GetUserInfo(uid, key); userInfo != nil {
		logs := userInfo.GetAllActivity()
		logsJSON, err := json.Marshal(logs)
		if err != nil {
			w.WriteHeader(500)
			w.Write([]byte("User logs can't be converted to JSON"))
			return
		}

		w.WriteHeader(200)
		w.Write(logsJSON)
	} else {
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprint("Unauthorized")))
	}
}

// Disable Disables an account disallowing any future access to it
func (mg *Manager) Disable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	uid := r.FormValue("u")
	key := r.FormValue("k")

	if userInfo := mg.usersModel.GetUserInfo(uid, key); userInfo != nil {
		userInfo.DisableUser()
		userInfo.AddActivityLog(users.CActivityAccountType, "Account disabled", r.RemoteAddr)
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprint("Unauthorized")))
	}
}

// ChangePass Verify and changes the password for a speciied account
func (mg *Manager) ChangePass(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var recSignatue string

	uid := r.FormValue("u")
	key := r.FormValue("k")
	newKey := r.FormValue("nk")
	signature := r.FormValue("s")
	if signature != "" {
		ttl, err := strconv.ParseInt(r.FormValue("t"), 10, 64)
		if err != nil {
			w.WriteHeader(500)
			w.Write([]byte("Internal Server Error"))
			return
		}
		recSignatue = mg.getSignature(uid, "recovery", ttl)
	}

	if userInfo := mg.usersModel.GetUserInfo(uid, key); userInfo != nil || (signature != "" && signature == recSignatue) {
		if userInfo.UpdateUser(newKey) {
			userInfo.AddActivityLog(users.CActivityAccountType, "Password changed", r.RemoteAddr)
			w.WriteHeader(200)
			w.Write([]byte("OK"))
		} else {
			w.WriteHeader(500)
			w.Write([]byte("Internal Server Error"))
		}
	} else {
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprint("Unauthorized")))
	}
}

// RecoverPass This method generates a "recovery token" that is sent to the
// account associated e-mail, from the link provided on the e-mail the user can
// restore the account password
func (mg *Manager) RecoverPass(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	uid := r.FormValue("u")

	if userInfo := mg.usersModel.AdminGetUserInfoByID(uid); userInfo != nil {
		ttl := time.Now().Unix() + cConfirmEmailTTL

		v := url.Values{}
		v.Set("u", uid)
		v.Set("t", fmt.Sprintf("%d", ttl))
		v.Set("s", mg.getSignature(uid, "recovery", ttl))

		verifURL := fmt.Sprintf(
			"%s/%s?%s",
			mg.baseURL,
			CResetPass,
			v.Encode())

		body := fmt.Sprintf(
			"Hi!,\n\tYou have requested password recovery, please click the following link to reset your password: %s\n\nBest,",
			verifURL)

		if mg.SendEmail(uid, body, "Pitia: Password Recovery") {
			userInfo.AddActivityLog(users.CActivityAccountType, "Password recovery sent", r.RemoteAddr)
			w.WriteHeader(200)
			w.Write([]byte("OK"))
			return
		}

		w.WriteHeader(500)
		w.Write([]byte("KO"))
		return
	}

	// Don't return any clue about if the user is or is not registered
	w.WriteHeader(200)
	w.Write([]byte("OK"))
	return
}

// getSignature Generates a signature from the provided parameters that can be
// used to verify or sign this
func (mg *Manager) getSignature(uid, keyHash string, ttl int64) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("%s:%s:%d:%s", uid, keyHash, ttl, mg.secret))))
}

// SendEmail Sends an e-mail to the specified addess with the specified subject
// and body
func (mg *Manager) SendEmail(to, body, subject string) (success bool) {
	auth := smtp.PlainAuth(
		mg.mailFromAddr,
		mg.mailFromAddr,
		os.Getenv("PIT_MAIL_PASS"),
		mg.mailServerAddr,
	)

	body = fmt.Sprintf(
		"Subject: %s\r\n\r\n\r\n%s",
		subject,
		[]byte(body))

	err := smtp.SendMail(
		fmt.Sprintf("%s:%d", mg.mailServerAddr, mg.mailServerPort),
		auth,
		mg.mailFromAddr,
		[]string{to},
		[]byte(body),
	)

	if err != nil {
		log.Error("Problem trying to send a verification e-mail:", err)
		return false
	}
	return true
}
