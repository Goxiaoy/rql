package integration

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/a8m/rql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	CreateTime, _ = time.Parse(time.RFC3339, "2000-05-16T16:00:00.000Z")
	MySQLConn     = os.Getenv("MYSQL_DSN")
	QueryParser   = rql.MustNewParser(rql.Config{
		Model:    User{},
		FieldSep: ".",
	})
)

type User struct {
	ID           int        `rql:"filter,sort"`
	Admin        bool       `rql:"filter"`
	Name         string     `rql:"filter"`
	AddressName  string     `rql:"filter"`
	CreatedAt    time.Time  `rql:"filter"`
	UnixTime     time.Time  `rql:"filter,layout=UnixDate"`
	CustomTime   time.Time  `rql:"filter,layout=2006-01-02 15:04"`
	NullableTime *time.Time `rql:"filter,layout=2006-01-02 15:04"`
}

func TestDB(t *testing.T) {
	db := Connect(t).Debug()
	SetUp(t, db)
	defer Teardown(t, db)
	AssertCount(t, db, 1, `{ "filter": { "id": 1 } }`)
	AssertCount(t, db, 1, `{ "filter": { "id": 100 } }`)
	AssertCount(t, db, 3, `{ "filter": { "id": { "$in": [1,3,5] } } }`)
	AssertCount(t, db, 6, `{ "filter": { "id": { "$nin": [1,3,5,7], "$lte": 10 } } }`)
	AssertCount(t, db, 2, `{ "filter": {"name": {"$in":["user_1","user_2"]} } }`)
	AssertCount(t, db, 50, `{ "filter": { "id": { "$gt": 50 } } }`)
	AssertCount(t, db, 50, `{ "filter": { "id": { "$lte": 50 } } }`)
	AssertCount(t, db, 99, `{ "filter": { "$or": [{ "id":{ "$gt": 50 } }, { "id":{ "$lt": 50 } }] } }`)
	AssertCount(t, db, 25, `{ "filter": { "nullable_time": {"$null": true } } }`)
	AssertCount(t, db, 75, `{ "filter": { "nullable_time": {"$nnull": true } } }`)
	AssertCount(t, db, 1, `{ "filter": {"name": "user_1" } }`)
	AssertCount(t, db, 100, `{ "filter": {"name": {"$like": "user%" } } }`) // all
	AssertCount(t, db, 2, `{ "filter": {"name": {"$like": "%10%" } } }`)    // 10 or 100
	AssertCount(t, db, 50, `{ "filter": {"admin": true } }`)                // 50 users
	AssertCount(t, db, 0, `{ "filter": {"address_name": "??" } }`)          // nothing
	AssertCount(t, db, 1, `{ "filter": {"address_name": "address_1" } }`)   // 1st user
	AssertCount(t, db, 100, fmt.Sprintf(`{"filter": {"created_at": { "$gt": %q } } }`, CreateTime.Add(-time.Hour).Format(time.RFC3339)))
	AssertCount(t, db, 100, fmt.Sprintf(`{"filter": {"created_at": { "$lte": %q } } }`, CreateTime.Add(time.Hour).Format(time.RFC3339)))
	AssertCount(t, db, 100, fmt.Sprintf(`{"filter": {"unix_time": { "$gt": %q } } }`, CreateTime.Add(-time.Hour).Format(time.UnixDate)))
	AssertCount(t, db, 100, fmt.Sprintf(`{"filter": {"unix_time": { "$lte": %q } } }`, CreateTime.Add(time.Hour).Format(time.UnixDate)))
	AssertCount(t, db, 100, fmt.Sprintf(`{"filter": {"custom_time": { "$gt": %q } } }`, CreateTime.Add(-time.Hour).Format("2006-01-02 15:04")))
	AssertCount(t, db, 100, fmt.Sprintf(`{"filter": {"custom_time": { "$lte": %q } } }`, CreateTime.Add(time.Hour).Format("2006-01-02 15:04")))
	AssertMatchIDs(t, db, []int{1}, `{ "filter": { "id": 1 } }`)
	AssertMatchIDs(t, db, []int{1, 3, 5}, `{ "filter": { "id": { "$in": [1,3,5] } } }`)
	AssertMatchIDs(t, db, []int{2, 4, 6, 8, 9, 10}, `{ "filter": { "id": { "$nin": [1,3,5,7], "$lte": 10 } } }`)
	AssertMatchIDs(t, db, []int{2, 3}, `{ "filter": { "$or": [ { "id": 2 }, { "id": 3 } ] } }`)
	AssertMatchIDs(t, db, []int{3, 2}, `{ "filter": { "$or": [ { "id": 2 }, { "id": 3 } ] }, "sort": ["-id"] }`)
	AssertMatchIDs(t, db, []int{5, 4, 3, 2, 1}, `{ "filter": { "id": { "$lte": 5 } }, "sort": ["-id"] }`)
	AssertSelect(t, db, []string{"user_1", "user_2"}, `{ "select": ["name"], "limit": 2 }`)
	AssertSelect(t, db, []string{"address_1", "address_2"}, `{ "select": ["address_name"], "limit": 2 }`)
}

func AssertCount(t *testing.T, db *gorm.DB, expected int64, query string) {
	params, err := QueryParser.Parse([]byte(query))
	must(t, err, "parse query: %s", query)
	var count int64 = 0
	err = db.Model(User{}).
		Where(params.FilterExp, params.FilterArgs...).
		Count(&count).Error
	must(t, err, "count users")
	if count != expected {
		t.Errorf("AssertCount: %s\n\twant: %d\n\tgot: %d", query, expected, count)
	}
}

func AssertMatchIDs(t *testing.T, db *gorm.DB, expected []int, query string) {
	params, err := QueryParser.Parse([]byte(query))
	must(t, err, "parse query: %s", query)
	var ids []int
	err = db.Model(User{}).
		Where(params.FilterExp, params.FilterArgs...).
		Order(params.Sort).
		Pluck("id", &ids).Error
	must(t, err, "select ids")
	if len(ids) != len(expected) {
		t.Errorf("AssertMatchIDs:\n\twant: %v\n\tgot: %v", expected, ids)
		return
	}
	for i := range expected {
		if ids[i] != expected[i] {
			t.Errorf("AssertMatchIDs:\n\twant: %v\n\tgot: %v", expected, ids)
			return
		}
	}
}

func AssertSelect(t *testing.T, db *gorm.DB, expected []string, query string) {
	params, err := QueryParser.Parse([]byte(query))
	must(t, err, "parse query: %s", query)
	var values []string
	err = db.Model(User{}).
		Limit(params.Limit).
		Select(params.Select).
		Pluck(params.Select, &values).Error
	must(t, err, "select values")
	if len(values) != len(expected) {
		t.Errorf("AssertSelect:\n\twant: %v\n\tgot: %v", expected, values)
		return
	}
	for i := range expected {
		if values[i] != expected[i] {
			t.Errorf("AssertSelect:\n\twant: %v\n\tgot: %v", expected, values)
			return
		}
	}
}

func Connect(t *testing.T) *gorm.DB {
	if MySQLConn == "" {
		db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"))
		if err == nil {
			sqlDB, err := db.DB()
			if err == nil {
				sqlDB.SetMaxIdleConns(1)
				sqlDB.SetMaxOpenConns(1)
				return db
			}
		}
	}
	for i := 1; i <= 5; i++ {
		db, err := gorm.Open(mysql.Open(MySQLConn))
		if err == nil {
			return db
		}
		time.Sleep(time.Second * time.Duration(i))
	}
	t.Log("failed connect to the database")
	return nil
}

func SetUp(t *testing.T, db *gorm.DB) {
	must(t, db.AutoMigrate(User{}), "migrate db")
	var wg sync.WaitGroup
	wg.Add(100)
	for i := 1; i <= 100; i++ {
		go func(i int) {
			defer wg.Done()
			var nt *time.Time
			if i%4 != 0 {
				nt = &CreateTime
			}
			err := db.Create(&User{
				ID:           i,
				Admin:        i%2 == 0,
				Name:         fmt.Sprintf("user_%d", i),
				AddressName:  fmt.Sprintf("address_%d", i),
				CreatedAt:    CreateTime.Add(time.Minute),
				UnixTime:     CreateTime.Add(time.Minute),
				CustomTime:   CreateTime.Add(time.Minute),
				NullableTime: nt,
			}).Error
			must(t, err, "create user")
		}(i)
	}
	wg.Wait()
}

func Teardown(t *testing.T, db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("can not close")
	}
	must(t, db.Migrator().DropTable(User{}), "drop table")
	must(t, sqlDB.Close(), "close conn to mysql")
}

func must(t *testing.T, err error, msg string, args ...interface{}) {
	if err != nil {
		args = append(args, err)
		t.Fatalf(msg+": %s", args...)
	}
}
