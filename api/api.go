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
)

type Api struct {
	shardsManager   *shardsmanager.Manager
	accountsManager *accountsmanager.Manager

	muxHttpServer *http.ServeMux
}

func Init(shardsManager *shardsmanager.Manager, accountsManager *accountsmanager.Manager) (api *Api) {
	api = &Api{
		shardsManager:   shardsManager,
		accountsManager: accountsManager,
		muxHttpServer:   http.NewServeMux(),
	}

	api.registerApis()

	log.Info("Starting API server on port:", int(cfg.GetInt("rec-api", "port")))
	go http.ListenAndServe(fmt.Sprintf(":%d", int(cfg.GetInt("rec-api", "port"))), api.muxHttpServer)

	return
}

func (api *Api) registerApis() {
	api.muxHttpServer.HandleFunc(cHealtyPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	})

	api.muxHttpServer.HandleFunc(shardsmanager.CRecPath, api.shardsManager.ScoresApiHandler)
	api.muxHttpServer.HandleFunc(shardsmanager.CGroupInfoPath, api.shardsManager.GroupInfoApiHandler)

	api.muxHttpServer.HandleFunc(accountsmanager.CRecPath, api.accountsManager.Register)
	api.muxHttpServer.HandleFunc(accountsmanager.CVerifyPath, api.accountsManager.Verify)
	api.muxHttpServer.HandleFunc(accountsmanager.CRecoverPassPath, api.accountsManager.RecoverPass)
	api.muxHttpServer.HandleFunc(accountsmanager.CInfoPath, api.accountsManager.Info)
	api.muxHttpServer.HandleFunc(accountsmanager.CDisablePath, api.accountsManager.Disable)
}
