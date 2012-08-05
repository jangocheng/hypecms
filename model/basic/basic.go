package basic

import(
	ifaces "github.com/opesun/hypecms/interfaces"
	"github.com/opesun/slugify"
	"labix.org/v2/mgo/bson"
	"labix.org/v2/mgo"
	"fmt"
	"time"
)

const(
	Created_by			= "_users_created_by"
	Created				= "created"
	Last_modified_by	= "_users_last_modified_by"
	Last_modified		= "last_modified"
	Version_datefield	= "version_date"
	Version_parentfield	= "version_parent"
)

// by Id.
func Find(db *mgo.Database, coll, id string) map[string]interface{} {
	var v interface{}
	db.C("users").Find(bson.M{"_id": bson.ObjectIdHex(id)}).One(&v)
	if v != nil {
		return Convert(v).(map[string]interface{})
	}
	return nil
}

// Insert, and update/delete, by Id.
func Inud(db *mgo.Database, ev ifaces.Event, dat map[string]interface{}, coll, op, id string) error {
	return InudOpt(db, ev, dat, coll, op, id, false)
}

func InudVersion(db *mgo.Database, ev ifaces.Event, dat map[string]interface{}, coll, op, id string) error {
	return InudOpt(db, ev, dat, coll, op, id, true)
}

func InudOpt(db *mgo.Database, ev ifaces.Event, dat map[string]interface{}, coll, op, id string, version bool) error {
	var err error
	if (op == "update" || op == "delete") && len(id) != 24 {
		if len(id) == 39 {
			id = id[13:37]
		} else {
			return fmt.Errorf("Length of id is not 24 or 39 at updating or deleting.")
		}
	}
	switch op {
	case "insert":
		dat["_id"] = bson.NewObjectId()
		err = db.C(coll).Insert(dat)
	case "update":
		if version {
			// Damn, mongodb does not allow to update the _id of a document.
			var v interface{}
			old_id := bson.ObjectIdHex(id)
			err = db.C(coll).Find(bson.M{"_id":old_id}).One(&v)
			if err != nil { return err }
			if v == nil { return fmt.Errorf("Can't find document at update.") }
			old_doc := v.(bson.M)
			old_doc["_id"] = bson.NewObjectId()
			old_doc[Version_parentfield] = old_id
			old_doc[Version_datefield] = time.Now().Unix()
			if typ, has_typ := old_doc["type"]; has_typ {
				old_doc["type"] = typ.(string) + "_version"
			}
			err = db.C(coll).Insert(old_doc)
			if err != nil { return err }
			q := bson.M{"_id": old_id}
			upd := bson.M{"$set": dat}
			err = db.C(coll).Update(q, upd)
		} else {
			q := bson.M{"_id": bson.ObjectIdHex(id)}
			upd := bson.M{"$set": dat}
			err = db.C(coll).Update(q, upd)
		}
	case "delete":
		var v interface{}
		err = db.C(coll).Find(bson.M{"_id": bson.ObjectIdHex(id)}).One(&v)
		if v == nil {
			return fmt.Errorf("Can't find document " + id + " in " + coll)
		}
		if err != nil {
			return err
		}
		// Transactions would not hurt here, but maybe we can get away with upserts.
		q := bson.M{"_id": bson.ObjectIdHex(id)}
		_, err = db.C(coll + "_deleted").Upsert(q, v)
		if err != nil {
			return err
		}
		q = bson.M{"_id": bson.ObjectIdHex(id)}
		err = db.C(coll).Remove(q)
		if err != nil {
			return err
		}
	case "restore":
		// err = db.C(coll).Find
		// Not implemented yet.
	}
	ev.Trigger(coll + "." + op, dat)
	return nil
}

func Convert(x interface{}) interface{} {
	if y, ok := x.(bson.M); ok {
		for key, val := range y {
			y[key] = Convert(val)
		}
		return (map[string]interface{})(y)
	} else if d, ok := x.(map[string]interface{}); ok {
		for key, val := range d {
			d[key] = Convert(val)
		}
		return d
	} else if z, ok := x.([]interface{}); ok {
		for i, v := range z {
			z[i] = Convert(v)
		}
	}
	return x
}

func convertMapId(x map[string]interface{}) {
	if id, has_id := x["_id"]; has_id {
		x["_id"] = id.(bson.ObjectId).Hex()
	}
}

// Used in views, where we dont want to show display ObjectIdHex("ab889d8ec889") but rather "ab889d8ec889".
// x must be a result map[string]interface{} or result []interface{}
func IdsToStrings(x interface{}) {
	if ma, ok := x.(map[string]interface{}); ok {
		convertMapId(ma)
	} else if ma_sl, sl_ok := x.([]interface{}); sl_ok {
		for _, v := range ma_sl {
			convertMapId(v.(map[string]interface{}))
		}
	} else {
		panic("Wrong input for IdsToString.")
	}
}

// Takes a query map and converts the "_id" member of it from string to bson.ObjectId
func StringToId(query map[string]interface{}) {
	if id_i, has := query["_id"]; has {
		if id_s, is_str := id_i.(string); is_str {
			query["_id"] = bson.ObjectIdHex(id_s)
		}
	}
}

func CreateCopy(db *mgo.Database, collname, sortfield string) bson.ObjectId {
	var v interface{}
	db.C(collname).Find(nil).Sort(sortfield).Limit(1).One(&v)
	ma := v.(bson.M)
	ma["_id"] = bson.NewObjectId()
	ma["created"] = time.Now().Unix()
	db.C(collname).Insert(ma)
	return ma["_id"].(bson.ObjectId)
}

// Called before install and uninstall automatically, and you must call it by hand every time you want to modify the option document.
func CreateOptCopy(db *mgo.Database) bson.ObjectId {
	return CreateCopy(db, "options", "-created")
}

// Calculate missing fields, we compare dat to r.
func CalcMiss(rule map[string]interface{}, dat map[string]interface{}) []string {
	missing_fields := []string{}
	for i, _ := range rule {
		if _, ex := dat[i]; !ex {
			missing_fields = append(missing_fields, i)
		}
	}
	return missing_fields
}

// Kind of an extension of the extract module.
// Handles author fields 	(created_by, last_modified_by),
// And time fields			(created, last_modified)
func DateAndAuthor(rule map[string]interface{}, dat map[string]interface{}, user_id bson.ObjectId, updating bool) {
	inserting := !updating
	for i, _ := range rule {
		switch i {
		case Created_by:
			if inserting {
				dat[i] = user_id
			}
		case Last_modified_by:
			if updating {
				dat[i] = user_id
			}
		case Created:
			if inserting {
				dat[i] = time.Now().Unix()
			}
		case Last_modified:
			fmt.Println("lol")
			if updating {
				dat[i] = time.Now().Unix()
			}
		}
	}
}

// If rule has slugs, then dat will already contain it if it was sent from UI.
func Slug(rule map[string]interface{}, dat map[string]interface{}) {
	_, has_slug := rule["slug"]
	if has_slug {
		if slug, has := dat["slug"]; has && len(slug.(string)) > 0 {
			return
		} else if name, has_name := dat["name"]; has_name {
			dat["slug"] = slugify.S(name.(string))
		} else if title, has_title := dat["title"]; has_title {
			dat["slug"] = slugify.S(title.(string))
		}
	}
}

func StripId(str_id string) string {
	l := len(str_id)
	if l != 24 {
		if l < 38 { panic("Bad id at basic.StripId.") }
		return str_id[13:37]
	}
	return str_id
}

func ExtractIds(dat map[string][]string, keys []string) ([]string, error) {
	ret := []string {}
	for _, v := range keys {
		id_s, has_id_s := dat[v]
		if !has_id_s {
			return nil, fmt.Errorf(v, " id is not found.")
		}
		id := id_s[0]
		if len(id) == 24 {
			ret = append(ret, v)
		} else if len(id) == 39 {
			ret = append(ret, id[13:37])
		} else {
			return nil, fmt.Errorf(v, " is not a proper id.")
		}
	}
	return ret, nil
}