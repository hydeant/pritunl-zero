package proxy

import (
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/dropbox/godropbox/container/set"
	"github.com/dropbox/godropbox/errors"
	"github.com/pritunl/mongo-go-driver/bson/primitive"
	"github.com/hydeant/pritunl-zero/audit"
	"github.com/hydeant/pritunl-zero/auth"
	"github.com/hydeant/pritunl-zero/authority"
	"github.com/hydeant/pritunl-zero/authorizer"
	"github.com/hydeant/pritunl-zero/database"
	"github.com/hydeant/pritunl-zero/errortypes"
	"github.com/hydeant/pritunl-zero/node"
	"github.com/hydeant/pritunl-zero/service"
	"github.com/hydeant/pritunl-zero/session"
	"github.com/hydeant/pritunl-zero/settings"
	"github.com/hydeant/pritunl-zero/utils"
	"github.com/hydeant/pritunl-zero/validator"
)

type Host struct {
	Service           *service.Service
	Domain            *service.Domain
	WhitelistNetworks []*net.IPNet
	ClientAuthority   *authority.Authority
	ClientCertificate *tls.Certificate
}

type Proxy struct {
	Hosts     map[string]*Host
	nodeHash  []byte
	wProxies  map[string][]*web
	wsProxies map[string][]*webSocket
	wiProxies map[string][]*webIsolated
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) bool {
	hst := utils.StripPort(r.Host)

	host := p.Hosts[hst]
	wProxies := p.wProxies[hst]
	wsProxies := p.wsProxies[hst]
	wiProxies := p.wiProxies[hst]

	wLen := 0
	if wProxies != nil {
		wLen = len(wProxies)
	}

	wsLen := 0
	if wsProxies != nil {
		wsLen = len(wsProxies)
	}

	wiLen := 0
	if wiProxies != nil {
		wiLen = len(wiProxies)
	}

	if host == nil || wLen == 0 || wProxies == nil {
		if r.URL.Path == "/check" {
			utils.WriteText(w, 200, "ok")
			return true
		}

		utils.WriteStatus(w, 404)
		return true
	}

	if !host.Service.DisableCsrfCheck {
		valid := auth.CsrfCheck(w, r, host.Domain.Domain)
		if !valid {
			return true
		}
	}

	db := database.GetDatabase()
	defer db.Close()

	remoteAddr, addrHeader, addrValid := node.Self.SafeGetRemoteAddr(r)
	if !addrValid && len(host.WhitelistNetworks) > 0 {
		logrus.WithFields(logrus.Fields{
			"service_id": host.Service.Id.Hex(),
		}).Error("proxy: Unsafe access on whitelisted networks " +
			"with unset forwarded header. Disabling whitelisted networks")

		err := host.Service.RemoveWhitelistNetworks()
		if err != nil {
			WriteError(w, r, 500, err)
			return true
		}

		host.WhitelistNetworks = []*net.IPNet{}
	} else if addrValid && len(host.WhitelistNetworks) > 0 {
		if addrHeader && !settings.Router.UnsafeRemoteHeader &&
			!utils.IsPrivateRequest(r) {

			logrus.WithFields(logrus.Fields{
				"service_id":            host.Service.Id.Hex(),
				"remote_address":        utils.StripPort(r.RemoteAddr),
				"header_remote_address": remoteAddr,
			}).Error("proxy: Blocking remote header address " +
				"whitelist check")
		} else {
			clientIp := net.ParseIP(remoteAddr)
			if clientIp != nil {
				for _, network := range host.WhitelistNetworks {
					if network.Contains(clientIp) {
						if wsProxies != nil && wsLen > 0 &&
							r.Header.Get("Upgrade") == "websocket" {

							wsProxies[rand.Intn(wsLen)].ServeHTTP(
								w, r, db, authorizer.NewProxy(nil))
							return true
						}

						wProxies[rand.Intn(wLen)].ServeHTTP(
							w, r, authorizer.NewProxy(nil))
						return true
					}
				}
			}
		}
	}

	if wiProxies != nil && wiLen > 0 &&
		host.Service.MatchWhitelistPath(r.URL.Path) {

		wiProxies[rand.Intn(wLen)].ServeHTTP(
			w, r, authorizer.NewProxy(nil))
		return true
	}

	authr, err := authorizer.AuthorizeProxy(db, host.Service, w, r)
	if err != nil {
		WriteError(w, r, 500, err)
		return true
	}

	if !authr.IsValid() {
		err = authr.Clear(db, w, r)
		if err != nil {
			WriteError(w, r, 500, err)
			return true
		}

		return false
	}

	usr, err := authr.GetUser(db)
	if err != nil {
		WriteError(w, r, 500, err)
		return true
	}

	if usr == nil {
		err = authr.Clear(db, w, r)
		if err != nil {
			WriteError(w, r, 500, err)
			return true
		}

		return false
	}

	active, err := auth.SyncUser(db, usr)
	if err != nil {
		WriteError(w, r, 500, err)
		return true
	}

	if !active {
		err = session.RemoveAll(db, usr.Id)
		if err != nil {
			WriteError(w, r, 500, err)
			return true
		}

		err = authr.Clear(db, w, r)
		if err != nil {
			WriteError(w, r, 500, err)
			return true
		}

		return false
	}

	_, _, errAudit, errData, err := validator.ValidateProxy(
		db, usr, authr.IsApi(), host.Service, r)
	if err != nil {
		WriteError(w, r, 500, err)
		return true
	}

	if errData != nil {
		err = authr.Clear(db, w, r)
		if err != nil {
			WriteError(w, r, 500, err)
			return true
		}

		if errAudit == nil {
			errAudit = audit.Fields{
				"error":   errData.Error,
				"message": errData.Message,
			}
		}
		errAudit["method"] = "check"

		err = audit.New(
			db,
			r,
			usr.Id,
			audit.ProxyAuthFailed,
			errAudit,
		)
		if err != nil {
			WriteError(w, r, 500, err)
			return true
		}

		return false
	}

	if wsProxies != nil && r.Header.Get("Upgrade") == "websocket" {
		wsProxies[rand.Intn(wsLen)].ServeHTTP(w, r, db, authr)
		return true
	}

	if host.Service.MatchLogoutPath(r.URL.Path) {
		err = authr.Clear(db, w, r)
		if err != nil {
			WriteError(w, r, 500, err)
			return true
		}

		http.Redirect(w, r, "/", 302)
		return true
	}

	wProxies[rand.Intn(wLen)].ServeHTTP(w, r, authr)
	return true
}

func (p *Proxy) reloadHosts(db *database.Database,
	services []primitive.ObjectID) (err error) {

	hosts := map[string]*Host{}
	appId := ""
	facets := []string{}

	if node.Self.UserDomain != "" {
		appId = fmt.Sprintf("https://%s/auth/u2f/app.json",
			node.Self.UserDomain)
	}

	nodeServices := set.NewSet()
	for _, srvc := range services {
		nodeServices.Add(srvc)
	}

	nodes, err := node.GetAll(db)
	if err != nil {
		return
	}

	for _, nde := range nodes {
		if appId == "" {
			appId = fmt.Sprintf("https://%s/auth/u2f/app.json",
				nde.UserDomain)
		}
		if nde.UserDomain != "" {
			facets = append(facets,
				fmt.Sprintf("https://%s", nde.UserDomain))
		}
		if nde.ManagementDomain != "" {
			facets = append(facets,
				fmt.Sprintf("https://%s", nde.ManagementDomain))
		}
	}

	srvcs, err := service.GetAll(db)
	if err != nil {
		p.Hosts = hosts
		return
	}

	for _, srvc := range srvcs {
		nodeService := nodeServices.Contains(srvc.Id)

		for _, domain := range srvc.Domains {
			facets = append(facets, fmt.Sprintf("https://%s", domain.Domain))

			if !nodeService {
				continue
			}
			whitelistNets := []*net.IPNet{}

			for _, cidr := range srvc.WhitelistNetworks {
				_, network, err := net.ParseCIDR(cidr)
				if err != nil {
					err = &errortypes.ParseError{
						errors.Wrap(err, "proxy: Failed to parse network"),
					}

					logrus.WithFields(logrus.Fields{
						"network": cidr,
						"error":   err,
					}).Error("proxy: Invalid whitelist network")
					err = nil

					continue
				}

				whitelistNets = append(whitelistNets, network)
			}

			var clientAuthr *authority.Authority
			if !srvc.ClientAuthority.IsZero() {
				clientAuthr, err = authority.Get(db, srvc.ClientAuthority)
				if err != nil {
					if _, ok := err.(*database.NotFoundError); ok {
						err = nil

						logrus.WithFields(logrus.Fields{
							"service_id":          srvc.Id.Hex(),
							"client_authority_id": srvc.ClientAuthority.Hex(),
						}).Warn("proxy: Service client authority not found")
					} else {
						return
					}
				}
			}

			var cert *tls.Certificate
			if clientAuthr != nil {
				cert, err = clientAuthr.CreateClientCertificate(db)
				if err != nil {
					return
				}
			}

			srvcDomain := &Host{
				Service:           srvc,
				Domain:            domain,
				WhitelistNetworks: whitelistNets,
				ClientAuthority:   clientAuthr,
				ClientCertificate: cert,
			}

			hosts[domain.Domain] = srvcDomain
		}
	}

	settings.Local.AppId = appId
	settings.Local.Facets = facets

	p.Hosts = hosts

	return
}

func (p *Proxy) reloadProxies(db *database.Database, proto string, port int) (
	err error) {

	wProxies := map[string][]*web{}
	wsProxies := map[string][]*webSocket{}
	wiProxies := map[string][]*webIsolated{}

	for domain, host := range p.Hosts {
		domainProxies := []*web{}
		for _, server := range host.Service.Servers {
			prxy := newWeb(proto, port, host, server)
			domainProxies = append(domainProxies, prxy)
		}
		wProxies[domain] = domainProxies

		if host.Service.WebSockets {
			domainWsProxies := []*webSocket{}
			for _, server := range host.Service.Servers {
				prxy := newWebSocket(proto, port, host, server)
				domainWsProxies = append(domainWsProxies, prxy)
			}
			wsProxies[domain] = domainWsProxies
		}

		domainIsoProxies := []*webIsolated{}
		for _, server := range host.Service.Servers {
			prxy := newWebIsolated(proto, port, host, server)
			domainIsoProxies = append(domainIsoProxies, prxy)
		}
		wiProxies[domain] = domainIsoProxies
	}

	p.wProxies = wProxies
	p.wsProxies = wsProxies
	p.wiProxies = wiProxies

	return
}

func (p *Proxy) update() (err error) {
	db := database.GetDatabase()
	defer db.Close()

	proto := node.Self.Protocol
	port := node.Self.Port
	services := node.Self.Services

	err = p.reloadHosts(db, services)
	if err != nil {
		return
	}

	err = p.reloadProxies(db, proto, port)
	if err != nil {
		return
	}

	return
}

func (p *Proxy) watchNode() {
	for {
		err := p.update()
		if err != nil {
			p.nodeHash = []byte{}
			p.Hosts = map[string]*Host{}
			p.wProxies = map[string][]*web{}
			p.wsProxies = map[string][]*webSocket{}
			p.wiProxies = map[string][]*webIsolated{}

			logrus.WithFields(logrus.Fields{
				"error": err,
			}).Error("proxy: Failed to load proxy state")
		}

		time.Sleep(3 * time.Second)
	}

	return
}

func (p *Proxy) Init() {
	p.Hosts = map[string]*Host{}
	p.wProxies = map[string][]*web{}
	p.wsProxies = map[string][]*webSocket{}
	go p.watchNode()
}
