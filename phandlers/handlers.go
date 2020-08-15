package phandlers

import (
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/hydeant/pritunl-zero/config"
	"github.com/hydeant/pritunl-zero/constants"
	"github.com/hydeant/pritunl-zero/middlewear"
	"github.com/hydeant/pritunl-zero/proxy"
	"github.com/hydeant/pritunl-zero/requires"
	"github.com/hydeant/pritunl-zero/service"
	"github.com/hydeant/pritunl-zero/static"
	"github.com/hydeant/pritunl-zero/utils"
)

var (
	index *static.File
	logo  *static.File
)

func Register(prxy *proxy.Proxy, engine *gin.Engine) {
	engine.Use(middlewear.Limiter)
	engine.Use(middlewear.Counter)
	engine.Use(middlewear.Recovery)

	engine.Use(func(c *gin.Context) {
		var srvc *service.Service
		host := prxy.Hosts[utils.StripPort(c.Request.Host)]
		if host != nil {
			srvc = host.Service
		}
		c.Set("service", srvc)
	})

	engine.NoRoute(redirect)

	dbGroup := engine.Group("")
	dbGroup.Use(middlewear.Database)

	sessGroup := dbGroup.Group("")
	sessGroup.Use(middlewear.SessionProxy)

	engine.GET("/auth/state", authStateGet)
	dbGroup.POST("/auth/session", authSessionPost)
	dbGroup.POST("/auth/secondary", authSecondaryPost)
	dbGroup.GET("/auth/request", authRequestGet)
	dbGroup.GET("/auth/callback", authCallbackGet)
	dbGroup.GET("/auth/u2f/sign", authU2fSignGet)
	dbGroup.POST("/auth/u2f/sign", authU2fSignPost)
	dbGroup.GET("/auth/u2f/register", authU2fRegisterGet)
	dbGroup.POST("/auth/u2f/register", authU2fRegisterPost)
	sessGroup.GET("/logout", logoutGet)

	engine.GET("/check", checkGet)

	engine.GET("/", staticIndexGet)
	engine.GET("/login", staticIndexGet)
	engine.GET("/logo.png", staticLogoGet)
	engine.GET("/robots.txt", middlewear.RobotsGet)
}

func init() {
	module := requires.New("phandlers")
	module.After("settings")

	module.Handler = func() (err error) {
		root := ""
		if constants.Production {
			root = config.StaticRoot
		} else {
			root = config.StaticTestingRoot
		}

		index, err = static.NewFile(filepath.Join(root, "login.html"))
		if err != nil {
			return
		}

		logo, err = static.NewFile(filepath.Join(root, "logo.png"))
		if err != nil {
			return
		}

		return
	}
}
