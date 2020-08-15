package cmd

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/dropbox/godropbox/errors"
	"github.com/pritunl/mongo-go-driver/bson/primitive"
	"github.com/hydeant/pritunl-zero/config"
	"github.com/hydeant/pritunl-zero/constants"
	"github.com/hydeant/pritunl-zero/errortypes"
	"github.com/hydeant/pritunl-zero/node"
	"github.com/hydeant/pritunl-zero/router"
	"github.com/hydeant/pritunl-zero/sync"
)

func Node() (err error) {
	objId, err := primitive.ObjectIDFromHex(config.Config.NodeId)
	if err != nil {
		err = &errortypes.ParseError{
			errors.Wrap(err, "cmd: Failed to parse ObjectId"),
		}
		return
	}

	nde := &node.Node{
		Id: objId,
	}
	err = nde.Init()
	if err != nil {
		return
	}

	sync.Init()

	routr := &router.Router{}

	routr.Init()

	go func() {
		err = routr.Run()
		if err != nil {
			panic(err)
		}
	}()

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	constants.Interrupt = true

	logrus.Info("cmd.node: Shutting down")
	go routr.Shutdown()
	if constants.Production {
		time.Sleep(8 * time.Second)
	} else {
		time.Sleep(300 * time.Millisecond)
	}

	return
}
