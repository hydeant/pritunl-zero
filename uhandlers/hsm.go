package uhandlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/dropbox/godropbox/container/set"
	"github.com/dropbox/godropbox/errors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/hydeant/pritunl-zero/authority"
	"github.com/hydeant/pritunl-zero/database"
	"github.com/hydeant/pritunl-zero/demo"
	"github.com/hydeant/pritunl-zero/errortypes"
	"github.com/hydeant/pritunl-zero/event"
	"github.com/hydeant/pritunl-zero/utils"
)

const (
	writeTimeout = 10 * time.Second
	pingInterval = 30 * time.Second
	pingWait     = 40 * time.Second
)

func hsmGet(c *gin.Context) {
	if demo.Blocked(c) {
		return
	}

	db := c.MustGet("db").(*database.Database)
	authr := c.MustGet("authority").(*authority.Authority)

	socket := &event.WebSocket{}

	defer func() {
		socket.Close()
		event.WebSocketsLock.Lock()
		event.WebSockets.Remove(socket)
		event.WebSocketsLock.Unlock()
	}()

	event.WebSocketsLock.Lock()
	event.WebSockets.Add(socket)
	event.WebSocketsLock.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	socket.Cancel = cancel

	conn, err := event.Upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		err = &errortypes.RequestError{
			errors.Wrap(err, "uhandlers: Failed to upgrade hsm request"),
		}
		utils.AbortWithError(c, 500, err)
		return
	}
	socket.Conn = conn

	conn.SetReadDeadline(time.Now().Add(pingWait))
	conn.SetPongHandler(func(x string) (err error) {
		conn.SetReadDeadline(time.Now().Add(pingWait))
		return
	})

	lst, err := event.SubscribeListener(db, []string{"pritunl_hsm_send"})
	if err != nil {
		utils.AbortWithError(c, 500, err)
		return
	}
	socket.Listener = lst

	ticker := time.NewTicker(pingInterval)
	socket.Ticker = ticker
	sub := lst.Listen()

	authr.HsmStatus = authority.Connected
	authr.HsmTimestamp = time.Now()
	authr.CommitFields(db, set.NewSet("hsm_status", "hsm_timestamp"))
	event.PublishDispatch(db, "authority.change")

	go func() {
		defer func() {
			if r := recover(); r != nil {
				if !socket.Closed {
					logrus.WithFields(logrus.Fields{
						"error": errors.New(fmt.Sprintf("%s", r)),
					}).Error("uhandlers: Socket hsm panic")
				}
			}
		}()

		lstDb := database.GetDatabase()
		defer lstDb.Close()

		for {
			_, message, e := conn.ReadMessage()
			if e != nil {
				logrus.WithFields(logrus.Fields{
					"error": e,
				}).Error("uhandlers: Socket hsm listen error")

				authr.HsmStatus = authority.Disconnected
				authr.CommitFields(db, set.NewSet("hsm_status"))
				event.PublishDispatch(db, "authority.change")

				conn.Close()
				break
			}

			payload := &authority.HsmPayload{}
			e = json.Unmarshal(message, payload)
			if e != nil {
				logrus.WithFields(logrus.Fields{
					"error": e,
				}).Error("uhandlers: Failed to unmarshal hsm payload")
				continue
			}

			if payload.Type == "status" {
				e = authr.HandleHsmStatus(lstDb, payload)
				if e != nil {
					logrus.WithFields(logrus.Fields{
						"error": e,
					}).Error("uhandlers: Failed to handle hsm status")
					continue
				}
			} else {
				e = event.Publish(lstDb, "pritunl_hsm_recv", payload)
				if e != nil {
					logrus.WithFields(logrus.Fields{
						"error": e,
					}).Error("uhandlers: Socket hsm publish event error")
					continue
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub:
			if !ok {
				conn.WriteControl(websocket.CloseMessage, []byte{},
					time.Now().Add(writeTimeout))
				return
			}

			conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			err = conn.WriteJSON(msg.Data)
			if err != nil {
				return
			}
		case <-ticker.C:
			err = conn.WriteControl(websocket.PingMessage, []byte{},
				time.Now().Add(writeTimeout))
			if err != nil {
				return
			}
		}
	}
}
