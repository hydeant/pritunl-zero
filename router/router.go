package router

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/dropbox/godropbox/errors"
	"github.com/gin-gonic/gin"
	"github.com/hydeant/pritunl-zero/acme"
	"github.com/hydeant/pritunl-zero/certificate"
	"github.com/hydeant/pritunl-zero/constants"
	"github.com/hydeant/pritunl-zero/errortypes"
	"github.com/hydeant/pritunl-zero/event"
	"github.com/hydeant/pritunl-zero/mhandlers"
	"github.com/hydeant/pritunl-zero/node"
	"github.com/hydeant/pritunl-zero/phandlers"
	"github.com/hydeant/pritunl-zero/proxy"
	"github.com/hydeant/pritunl-zero/settings"
	"github.com/hydeant/pritunl-zero/uhandlers"
	"github.com/hydeant/pritunl-zero/utils"
)

type Router struct {
	nodeHash         []byte
	typ              string
	port             int
	noRedirectServer bool
	protocol         string
	certificates     []*certificate.Certificate
	managementDomain string
	userDomain       string
	mRouter          *gin.Engine
	uRouter          *gin.Engine
	pRouter          *gin.Engine
	waiter           sync.WaitGroup
	lock             sync.Mutex
	redirectServer   *http.Server
	webServer        *http.Server
	proxy            *proxy.Proxy
	stop             bool
}

func (r *Router) ServeHTTP(w http.ResponseWriter, re *http.Request) {
	if node.Self.ForwardedProtoHeader != "" &&
		strings.ToLower(re.Header.Get(
			node.Self.ForwardedProtoHeader)) == "http" {

		re.URL.Host = utils.StripPort(re.Host)
		re.URL.Scheme = "https"

		http.Redirect(w, re, re.URL.String(),
			http.StatusMovedPermanently)
		return
	}

	hst := utils.StripPort(re.Host)
	if r.typ == node.Management {
		r.mRouter.ServeHTTP(w, re)
		return
	} else if r.typ == node.User {
		r.uRouter.ServeHTTP(w, re)
		return
	} else if strings.Contains(
		r.typ, node.Management) && hst == r.managementDomain {

		r.mRouter.ServeHTTP(w, re)
		return
	} else if strings.Contains(r.typ, node.User) && hst == r.userDomain {
		r.uRouter.ServeHTTP(w, re)
		return
	} else {
		if !r.proxy.ServeHTTP(w, re) {
			r.pRouter.ServeHTTP(w, re)
		}
		return
	}

	if re.URL.Path == "/check" {
		utils.WriteText(w, 200, "ok")
		return
	}

	utils.WriteStatus(w, 404)
}

func (r *Router) initRedirect() (err error) {
	r.redirectServer = &http.Server{
		Addr:           ":80",
		ReadTimeout:    1 * time.Minute,
		WriteTimeout:   1 * time.Minute,
		IdleTimeout:    1 * time.Minute,
		MaxHeaderBytes: 8192,
		Handler: http.HandlerFunc(func(
			w http.ResponseWriter, req *http.Request) {

			if strings.HasPrefix(req.URL.Path, acme.AcmePath) {
				token := acme.ParsePath(req.URL.Path)
				token = utils.FilterStr(token, 96)
				if token != "" {
					chal, err := acme.GetChallenge(token)
					if err != nil {
						utils.WriteStatus(w, 400)
					} else {
						logrus.WithFields(logrus.Fields{
							"token": token,
						}).Info("router: Acme challenge requested")
						utils.WriteText(w, 200, chal.Resource)
					}
					return
				}
			} else if req.URL.Path == "/check" {
				utils.WriteText(w, 200, "ok")
				return
			}

			newHost := utils.StripPort(req.Host)
			if r.port != 443 {
				newHost += fmt.Sprintf(":%d", r.port)
			}

			req.URL.Host = newHost
			req.URL.Scheme = "https"

			http.Redirect(w, req, req.URL.String(),
				http.StatusMovedPermanently)
		}),
	}

	return
}

func (r *Router) startRedirect() {
	defer r.waiter.Done()

	if r.port == 80 || r.noRedirectServer {
		return
	}

	logrus.WithFields(logrus.Fields{
		"production": constants.Production,
		"protocol":   "http",
		"port":       80,
	}).Info("router: Starting redirect server")

	err := r.redirectServer.ListenAndServe()
	if err != nil {
		if err == http.ErrServerClosed {
			err = nil
		} else {
			err = &errortypes.UnknownError{
				errors.Wrap(err, "router: Server listen failed"),
			}
			logrus.WithFields(logrus.Fields{
				"error": err,
			}).Error("router: Redirect server error")
		}
	}
}

func (r *Router) initWeb() (err error) {
	r.typ = node.Self.Type
	r.managementDomain = node.Self.ManagementDomain
	r.userDomain = node.Self.UserDomain
	r.certificates = node.Self.CertificateObjs
	r.noRedirectServer = node.Self.NoRedirectServer

	r.port = node.Self.Port
	if r.port == 0 {
		r.port = 443
	}

	r.protocol = node.Self.Protocol
	if r.protocol == "" {
		r.protocol = "https"
	}

	if strings.Contains(r.typ, node.Management) {
		r.mRouter = gin.New()

		if !constants.Production {
			r.mRouter.Use(gin.Logger())
		}

		mhandlers.Register(r.mRouter)
	}

	if strings.Contains(r.typ, node.User) {
		r.uRouter = gin.New()

		if !constants.Production {
			r.uRouter.Use(gin.Logger())
		}

		uhandlers.Register(r.uRouter)
	}

	if strings.Contains(r.typ, node.Proxy) {
		r.pRouter = gin.New()

		if !constants.Production {
			r.pRouter.Use(gin.Logger())
		}

		phandlers.Register(r.proxy, r.pRouter)
	}

	readTimeout := time.Duration(settings.Router.ReadTimeout) * time.Second
	readHeaderTimeout := time.Duration(
		settings.Router.ReadHeaderTimeout) * time.Second
	writeTimeout := time.Duration(settings.Router.WriteTimeout) * time.Second
	idleTimeout := time.Duration(settings.Router.IdleTimeout) * time.Second

	r.webServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", r.port),
		Handler:           r,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    4096,
	}

	if r.protocol != "http" &&
		(r.certificates == nil || len(r.certificates) == 0) {

		_, _, err = node.SelfCert()
		if err != nil {
			return
		}
	}

	return
}

func (r *Router) startWeb() {
	defer r.waiter.Done()

	logrus.WithFields(logrus.Fields{
		"production":          constants.Production,
		"protocol":            r.protocol,
		"port":                r.port,
		"read_timeout":        settings.Router.ReadTimeout,
		"write_timeout":       settings.Router.WriteTimeout,
		"idle_timeout":        settings.Router.IdleTimeout,
		"read_header_timeout": settings.Router.ReadHeaderTimeout,
	}).Info("router: Starting web server")

	if r.protocol == "http" {
		err := r.webServer.ListenAndServe()
		if err != nil {
			if err == http.ErrServerClosed {
				err = nil
			} else {
				err = &errortypes.UnknownError{
					errors.Wrap(err, "router: Server listen failed"),
				}
				logrus.WithFields(logrus.Fields{
					"error": err,
				}).Error("router: Web server error")
				return
			}
		}
	} else {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS13,
		}
		tlsConfig.Certificates = []tls.Certificate{}

		if r.certificates != nil {
			for _, cert := range r.certificates {
				keypair, err := tls.X509KeyPair(
					[]byte(cert.Certificate),
					[]byte(cert.Key),
				)
				if err != nil {
					err = &errortypes.ReadError{
						errors.Wrap(
							err,
							"router: Failed to load certificate",
						),
					}
					logrus.WithFields(logrus.Fields{
						"error": err,
					}).Error("router: Web server certificate error")
					err = nil
					continue
				}

				tlsConfig.Certificates = append(
					tlsConfig.Certificates,
					keypair,
				)
			}
		}

		if len(tlsConfig.Certificates) == 0 {
			certPem, keyPem, err := node.SelfCert()
			if err != nil {
				logrus.WithFields(logrus.Fields{
					"error": err,
				}).Error("router: Web server self certificate error")
				return
			}

			keypair, err := tls.X509KeyPair(certPem, keyPem)
			if err != nil {
				err = &errortypes.ReadError{
					errors.Wrap(
						err,
						"router: Failed to load self certificate",
					),
				}
				logrus.WithFields(logrus.Fields{
					"error": err,
				}).Error("router: Web server self certificate error")
				return
			}

			tlsConfig.Certificates = append(
				tlsConfig.Certificates,
				keypair,
			)
		}

		tlsConfig.BuildNameToCertificate()

		r.webServer.TLSConfig = tlsConfig

		listener, err := tls.Listen("tcp", r.webServer.Addr, tlsConfig)
		if err != nil {
			err = &errortypes.UnknownError{
				errors.Wrap(err, "router: TLS listen failed"),
			}
			logrus.WithFields(logrus.Fields{
				"error": err,
			}).Error("router: Web server TLS error")
			return
		}

		err = r.webServer.Serve(listener)
		if err != nil {
			if err == http.ErrServerClosed {
				err = nil
			} else {
				err = &errortypes.UnknownError{
					errors.Wrap(err, "router: Server listen failed"),
				}
				logrus.WithFields(logrus.Fields{
					"error": err,
				}).Error("router: Web server error")
				return
			}
		}
	}

	return
}

func (r *Router) initServers() (err error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	err = r.initRedirect()
	if err != nil {
		return
	}

	err = r.initWeb()
	if err != nil {
		return
	}

	return
}

func (r *Router) startServers() {
	r.lock.Lock()
	defer r.lock.Unlock()

	if r.redirectServer == nil || r.webServer == nil {
		return
	}

	r.waiter.Add(2)
	go r.startRedirect()
	go r.startWeb()

	time.Sleep(250 * time.Millisecond)

	return
}

func (r *Router) Restart() {
	r.lock.Lock()
	defer r.lock.Unlock()

	if r.redirectServer != nil {
		redirectCtx, redirectCancel := context.WithTimeout(
			context.Background(),
			1*time.Second,
		)
		defer redirectCancel()
		r.redirectServer.Shutdown(redirectCtx)
	}
	if r.webServer != nil {
		webCtx, webCancel := context.WithTimeout(
			context.Background(),
			1*time.Second,
		)
		defer webCancel()
		r.webServer.Shutdown(webCtx)
	}

	func() {
		defer func() {
			recover()
		}()
		if r.redirectServer != nil {
			r.redirectServer.Close()
		}
		if r.webServer != nil {
			r.webServer.Close()
		}
	}()

	event.WebSocketsStop()
	proxy.WebSocketsStop()

	r.redirectServer = nil
	r.webServer = nil

	time.Sleep(250 * time.Millisecond)
}

func (r *Router) Shutdown() {
	r.stop = true
	r.Restart()
	time.Sleep(1 * time.Second)
	r.Restart()
	time.Sleep(1 * time.Second)
	r.Restart()
}

func (r *Router) hashNode() []byte {
	hash := md5.New()
	io.WriteString(hash, node.Self.Type)
	io.WriteString(hash, node.Self.ManagementDomain)
	io.WriteString(hash, node.Self.UserDomain)
	io.WriteString(hash, strconv.Itoa(node.Self.Port))
	io.WriteString(hash, fmt.Sprintf("%t", node.Self.NoRedirectServer))
	io.WriteString(hash, node.Self.Protocol)

	io.WriteString(hash, strconv.Itoa(settings.Router.ReadTimeout))
	io.WriteString(hash, strconv.Itoa(settings.Router.ReadHeaderTimeout))
	io.WriteString(hash, strconv.Itoa(settings.Router.WriteTimeout))
	io.WriteString(hash, strconv.Itoa(settings.Router.IdleTimeout))

	certs := node.Self.CertificateObjs
	if certs != nil {
		for _, cert := range certs {
			io.WriteString(hash, cert.Hash())
		}
	}

	return hash.Sum(nil)
}

func (r *Router) watchNode() {
	for {
		time.Sleep(1 * time.Second)

		hash := r.hashNode()
		if bytes.Compare(r.nodeHash, hash) != 0 {
			r.nodeHash = hash
			time.Sleep(time.Duration(rand.Intn(3)) * time.Second)
			r.Restart()
			time.Sleep(2 * time.Second)
		}
	}

	return
}

func (r *Router) Run() (err error) {
	r.nodeHash = r.hashNode()
	go r.watchNode()

	for {
		err = r.initServers()
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"error": err,
			}).Error("router: Failed to init web servers")
			time.Sleep(1 * time.Second)
			continue
		}

		r.waiter = sync.WaitGroup{}
		r.startServers()
		r.waiter.Wait()

		if r.stop {
			break
		}
	}

	return
}

func (r *Router) Init() {
	if constants.Production {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	r.proxy = &proxy.Proxy{}
	r.proxy.Init()
}
