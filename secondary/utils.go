package secondary

import (
	"math/rand"
	"time"

	"github.com/pritunl/mongo-go-driver/bson"
	"github.com/pritunl/mongo-go-driver/bson/primitive"
	"github.com/hydeant/pritunl-zero/database"
	"github.com/hydeant/pritunl-zero/settings"
	"github.com/hydeant/pritunl-zero/utils"
)

func New(db *database.Database, userId primitive.ObjectID, typ string,
	proivderId primitive.ObjectID) (secd *Secondary, err error) {

	token, err := utils.RandStr(64)
	if err != nil {
		return
	}

	secd = &Secondary{
		Id:         token,
		UserId:     userId,
		Type:       typ,
		ProviderId: proivderId,
		Timestamp:  time.Now(),
	}

	err = secd.Insert(db)
	if err != nil {
		return
	}

	return
}

func NewChallenge(db *database.Database, userId primitive.ObjectID,
	typ string, chalId string, proivderId primitive.ObjectID) (
	secd *Secondary, err error) {

	token, err := utils.RandStr(64)
	if err != nil {
		return
	}

	secd = &Secondary{
		Id:          token,
		UserId:      userId,
		Type:        typ,
		ChallengeId: chalId,
		ProviderId:  proivderId,
		Timestamp:   time.Now(),
	}

	err = secd.Insert(db)
	if err != nil {
		return
	}

	return
}

func Get(db *database.Database, token string, typ string) (
	secd *Secondary, err error) {

	coll := db.SecondaryTokens()
	secd = &Secondary{}

	timestamp := time.Now().Add(
		-time.Duration(settings.Auth.SecondaryExpire) * time.Second)

	time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)

	err = coll.FindOne(db, &bson.M{
		"_id":  token,
		"type": typ,
		"timestamp": &bson.M{
			"$gte": timestamp,
		},
	}).Decode(secd)
	if err != nil {
		err = database.ParseError(err)
		return
	}

	return
}

func Remove(db *database.Database, token string) (err error) {
	coll := db.SecondaryTokens()

	_, err = coll.DeleteMany(db, &bson.M{
		"_id": token,
	})
	if err != nil {
		err = database.ParseError(err)
		return
	}

	return
}
