package policy

import (
	"github.com/pritunl/mongo-go-driver/bson"
	"github.com/pritunl/mongo-go-driver/bson/primitive"
	"github.com/hydeant/pritunl-zero/database"
)

func Get(db *database.Database, policyId primitive.ObjectID) (
	polcy *Policy, err error) {

	coll := db.Policies()
	polcy = &Policy{}

	err = coll.FindOneId(policyId, polcy)
	if err != nil {
		return
	}

	return
}

func GetService(db *database.Database, serviceId primitive.ObjectID) (
	policies []*Policy, err error) {

	coll := db.Policies()
	policies = []*Policy{}

	cursor, err := coll.Find(
		db,
		&bson.M{
			"services": serviceId,
		},
	)
	if err != nil {
		err = database.ParseError(err)
		return
	}
	defer cursor.Close(db)

	for cursor.Next(db) {
		polcy := &Policy{}
		err = cursor.Decode(polcy)
		if err != nil {
			err = database.ParseError(err)
			return
		}

		policies = append(policies, polcy)
	}

	err = cursor.Err()
	if err != nil {
		err = database.ParseError(err)
		return
	}

	return
}

func GetRoles(db *database.Database, roles []string) (
	policies []*Policy, err error) {

	coll := db.Policies()
	policies = []*Policy{}

	if roles == nil {
		roles = []string{}
	}

	cursor, err := coll.Find(
		db,
		&bson.M{
			"roles": &bson.M{
				"$in": roles,
			},
		},
	)
	if err != nil {
		err = database.ParseError(err)
		return
	}
	defer cursor.Close(db)

	for cursor.Next(db) {
		polcy := &Policy{}
		err = cursor.Decode(polcy)
		if err != nil {
			err = database.ParseError(err)
			return
		}

		policies = append(policies, polcy)
	}

	err = cursor.Err()
	if err != nil {
		err = database.ParseError(err)
		return
	}

	return
}

func GetAuthoritiesRoles(db *database.Database, authrIds []primitive.ObjectID,
	roles []string) (policies []*Policy, err error) {

	coll := db.Policies()
	policies = []*Policy{}

	cursor, err := coll.Find(
		db,
		&bson.M{
			"$or": []*bson.M{
				&bson.M{
					"roles": &bson.M{
						"$in": roles,
					},
				},
				&bson.M{
					"authorities": &bson.M{
						"$in": authrIds,
					},
				},
			},
		},
	)
	if err != nil {
		err = database.ParseError(err)
		return
	}
	defer cursor.Close(db)

	for cursor.Next(db) {
		polcy := &Policy{}
		err = cursor.Decode(polcy)
		if err != nil {
			err = database.ParseError(err)
			return
		}

		policies = append(policies, polcy)
	}

	err = cursor.Err()
	if err != nil {
		err = database.ParseError(err)
		return
	}

	return
}

func GetAll(db *database.Database) (policies []*Policy, err error) {
	coll := db.Policies()
	policies = []*Policy{}

	cursor, err := coll.Find(
		db,
		&bson.M{},
	)
	if err != nil {
		err = database.ParseError(err)
		return
	}
	defer cursor.Close(db)

	for cursor.Next(db) {
		polcy := &Policy{}
		err = cursor.Decode(polcy)
		if err != nil {
			err = database.ParseError(err)
			return
		}

		policies = append(policies, polcy)
	}

	err = cursor.Err()
	if err != nil {
		err = database.ParseError(err)
		return
	}

	return
}

func Remove(db *database.Database, policyId primitive.ObjectID) (err error) {
	coll := db.Policies()

	_, err = coll.DeleteMany(db, &bson.M{
		"_id": policyId,
	})
	if err != nil {
		err = database.ParseError(err)
		return
	}

	return
}
