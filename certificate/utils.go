package certificate

import (
	"github.com/pritunl/mongo-go-driver/bson"
	"github.com/pritunl/mongo-go-driver/bson/primitive"
	"github.com/hydeant/pritunl-zero/database"
)

func Get(db *database.Database, certId primitive.ObjectID) (
	cert *Certificate, err error) {

	coll := db.Certificates()
	cert = &Certificate{}

	err = coll.FindOneId(certId, cert)
	if err != nil {
		return
	}

	return
}

func GetAll(db *database.Database) (certs []*Certificate, err error) {
	coll := db.Certificates()
	certs = []*Certificate{}

	cursor, err := coll.Find(db, bson.M{})
	if err != nil {
		err = database.ParseError(err)
		return
	}
	defer cursor.Close(db)

	for cursor.Next(db) {
		cert := &Certificate{}
		err = cursor.Decode(cert)
		if err != nil {
			return
		}

		certs = append(certs, cert)
	}

	err = cursor.Err()
	if err != nil {
		err = database.ParseError(err)
		return
	}

	return
}

func Remove(db *database.Database, certId primitive.ObjectID) (err error) {
	coll := db.Certificates()

	_, err = coll.DeleteMany(db, &bson.M{
		"_id": certId,
	})
	if err != nil {
		err = database.ParseError(err)
		return
	}

	return
}
