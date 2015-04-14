package accountsmanager

import (
	"crypto/sha1"
	"fmt"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/models/users"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	CRecPath         = "/account_register"
	CVerifyPath      = "/account_verify"
	CInfoPath        = "/account_info"
	CDisablePath     = "/disable_account"
	CRecoverPassPath = "/recover_pass"

	cConfirmEmailTTL = 24 * 3600
)

type Manager struct {
	usersModel     *users.Model
	baseUrl        string
	secret         string
	mailFromAddr   string
	mailServerAddr string
	mailServerPort int64
}

func Init(baseUrl, prefix, region, mailFromAddr, mailServerAddr string, mailServerPort int64) (mg *Manager) {
	mg = &Manager{
		usersModel:     users.GetModel(prefix, region),
		baseUrl:        baseUrl,
		secret:         os.Getenv("PIT_SECRET"),
		mailServerAddr: mailServerAddr,
		mailFromAddr:   mailFromAddr,
		mailServerPort: mailServerPort,
	}

	return
}

func (mg *Manager) Register(w http.ResponseWriter, r *http.Request) {
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

	verifUrl := fmt.Sprintf(
		"%s/%s?%s",
		mg.baseUrl,
		CVerifyPath,
		v.Encode())

	emailSent := mg.sendEmail(
		uid,
		fmt.Sprintf(
			"Hello from Pitia!,\n\tPlease, click on the next link in order to verify you account: %s\n\nBest!,",
			verifUrl),
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
	uid := r.FormValue("u")
	key := r.FormValue("k")
	ts := r.FormValue("t")
	sign := r.FormValue("s")

	w.WriteHeader(200)
	w.Write([]byte(fmt.Sprint("Verify:", uid, key, ts, sign)))
}

func (mg *Manager) Info(w http.ResponseWriter, r *http.Request) {
}

func (mg *Manager) Disable(w http.ResponseWriter, r *http.Request) {
}

func (mg *Manager) RecoverPass(w http.ResponseWriter, r *http.Request) {
}

func (mg *Manager) getSignature(uid, keyHash string, ttl int64) string {
	return fmt.Sprintf("% x", sha1.Sum([]byte(fmt.Sprintf("%s:%s:%d:%s", uid, keyHash, ttl, mg.secret))))
}

func (mg *Manager) sendEmail(to, body, subject string) (success bool) {
	auth := smtp.PlainAuth(
		mg.mailFromAddr,
		mg.mailFromAddr,
		os.Getenv("PIT_MAIL_PASS"),
		mg.mailServerAddr,
	)

	body = fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\n%s", mg.mailFromAddr, to, subject, body)

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
