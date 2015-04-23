package api

import (
	"fmt"
	"github.com/alonsovidales/pit/accounts_manager"
	"github.com/alonsovidales/pit/cfg"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/shards_manager"
	"net/http"
)

const (
	cHealtyPath = "/check_healty"
	cContact    = "/contact"
)

type API struct {
	shardsManager   *shardsmanager.Manager
	accountsManager *accountsmanager.Manager
	staticPath      string

	muxHTTPServer *http.ServeMux
}

func Init(shardsManager *shardsmanager.Manager, accountsManager *accountsmanager.Manager, staticPath string, httpPort, httpsPort int, cert, key string) (api *API, sslAPI *API) {
	api = &API{
		shardsManager:   shardsManager,
		accountsManager: accountsManager,
		muxHTTPServer:   http.NewServeMux(),
		staticPath:      staticPath,
	}
	api.registerAPIs(false)
	log.Info("Starting API server on port:", httpPort)
	go http.ListenAndServe(fmt.Sprintf(":%d", httpPort), api.muxHTTPServer)

	// SSL Server, will not serve the /rec method by performance issues
	sslAPI = &API{
		shardsManager:   shardsManager,
		accountsManager: accountsManager,
		muxHTTPServer:   http.NewServeMux(),
		staticPath:      staticPath,
	}
	sslAPI.registerAPIs(true)
	log.Info("Starting SSL API server on port:", httpsPort)
	go http.ListenAndServeTLS(fmt.Sprintf(":%d", httpsPort), cert, key, sslAPI.muxHTTPServer)

	return
}

func (api *API) contact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	email := r.FormValue("mail")
	content := r.FormValue("content")

	if email != "" && content != "" {
		if api.accountsManager.SendEmail(cfg.GetStr("mail", "addr"), content, fmt.Sprintf("Contact form from: %s", email)) {
			w.WriteHeader(200)
			w.Write([]byte("OK"))
			return
		}
	}

	w.WriteHeader(500)
	w.Write([]byte("KO"))
	return
}

func (api *API) registerAPIs(ssl bool) {
	if !ssl {
		api.muxHTTPServer.HandleFunc(shardsmanager.CRecPath, api.shardsManager.ScoresAPIHandler)
		api.muxHTTPServer.HandleFunc(shardsmanager.CScoresPath, api.shardsManager.ScoresAPIHandler)
	}

	api.muxHTTPServer.HandleFunc(shardsmanager.CGroupInfoPath, api.shardsManager.GroupInfoAPIHandler)

	api.muxHTTPServer.HandleFunc(cHealtyPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	})

	api.muxHTTPServer.HandleFunc(shardsmanager.CRegenerateGroupKey, api.shardsManager.RegenerateGroupKey)
	api.muxHTTPServer.HandleFunc(shardsmanager.CGetGroupsByUser, api.shardsManager.GetGroupsByUser)
	api.muxHTTPServer.HandleFunc(shardsmanager.CAddUpdateGroup, api.shardsManager.AddUpdateGroup)
	api.muxHTTPServer.HandleFunc(shardsmanager.CSetShardsGroup, api.shardsManager.SetShards)
	api.muxHTTPServer.HandleFunc(shardsmanager.CRemoveShardsContent, api.shardsManager.RemoveShardsContent)

	api.muxHTTPServer.HandleFunc(accountsmanager.CRegisterPath, api.accountsManager.Register)
	api.muxHTTPServer.HandleFunc(accountsmanager.CVerifyPath, api.accountsManager.Verify)
	api.muxHTTPServer.HandleFunc(accountsmanager.CLogsPath, api.accountsManager.Logs)
	api.muxHTTPServer.HandleFunc(accountsmanager.CRecoverPassPath, api.accountsManager.RecoverPass)
	api.muxHTTPServer.HandleFunc(accountsmanager.CChangePass, api.accountsManager.ChangePass)
	api.muxHTTPServer.HandleFunc(accountsmanager.CDisablePath, api.accountsManager.Disable)

	api.muxHTTPServer.HandleFunc(cContact, api.contact)

	api.muxHTTPServer.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		filePath := r.URL.Path[1:]
		path := api.staticPath + filePath
		lastPosSlash := -1
		lastPosDot := -1

		for i := 0; i < len(path); i++ {
			switch path[i] {
			case '/':
				lastPosSlash = i
			case '.':
				lastPosDot = i
			}
		}

		if filePath != "" && lastPosDot < lastPosSlash {
			path += ".html"
		}

		http.ServeFile(w, r, path)
	})
}
