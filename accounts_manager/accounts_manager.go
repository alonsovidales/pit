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
	CRegisterPath    = "/account_register"
	CVerifyPath      = "/account_verify"
	CLogsPath        = "/account_logs"
	CDisablePath     = "/disable_account"
	CRecoverPassPath = "/recover_pass"
	CChangePass      = "/change_pass"
	CResetPass       = "/reset_pass" // HTML content

	cConfirmEmailTTL = 24 * 3600
)

type Manager struct {
	usersModel     users.ModelInt
	baseURL        string
	secret         string
	mailFromAddr   string
	mailServerAddr string
	mailServerPort int64
}

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

func (mg *Manager) getSignature(uid, keyHash string, ttl int64) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("%s:%s:%d:%s", uid, keyHash, ttl, mg.secret))))
}

func (mg *Manager) SendEmail(to, body, subject string) (success bool) {
	auth := smtp.PlainAuth(
		mg.mailFromAddr,
		mg.mailFromAddr,
		os.Getenv("PIT_MAIL_PASS"),
		mg.mailServerAddr,
	)

	body = fmt.Sprintf(
		"Subject: %s\r\n%s",
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
