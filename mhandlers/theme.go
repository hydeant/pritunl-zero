package mhandlers

import (
	"github.com/dropbox/godropbox/container/set"
	"github.com/gin-gonic/gin"
	"github.com/hydeant/pritunl-zero/authorizer"
	"github.com/hydeant/pritunl-zero/database"
	"github.com/hydeant/pritunl-zero/demo"
	"github.com/hydeant/pritunl-zero/utils"
)

type themeData struct {
	Theme string `json:"theme"`
}

func themePut(c *gin.Context) {
	if demo.IsDemo() {
		c.JSON(200, nil)
		return
	}

	db := c.MustGet("db").(*database.Database)
	authr := c.MustGet("authorizer").(*authorizer.Authorizer)
	data := &themeData{}

	err := c.Bind(&data)
	if err != nil {
		utils.AbortWithError(c, 500, err)
		return
	}

	usr, err := authr.GetUser(db)
	if err != nil {
		utils.AbortWithError(c, 500, err)
		return
	}

	usr.Theme = data.Theme

	err = usr.CommitFields(db, set.NewSet("theme"))
	if err != nil {
		utils.AbortWithError(c, 500, err)
		return
	}

	c.JSON(200, data)
	return
}
