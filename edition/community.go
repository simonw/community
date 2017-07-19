// Copyright 2016 Documize Inc. <legal@documize.com>. All rights reserved.
//
// This software (Documize Community Edition) is licensed under
// GNU AGPL v3 http://www.gnu.org/licenses/agpl-3.0.en.html
//
// You can operate outside the AGPL restrictions by purchasing
// Documize Enterprise Edition and obtaining a commercial license
// by contacting <sales@documize.com>.
//
// https://documize.com

// This package provides Documize as a single executable.
package main

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/documize/community/core/api"
	"github.com/documize/community/core/api/endpoint"
	"github.com/documize/community/core/api/request"
	"github.com/documize/community/core/database"
	"github.com/documize/community/core/env"
	"github.com/documize/community/core/section"
	"github.com/documize/community/core/web"
	"github.com/documize/community/edition/logging"
	_ "github.com/documize/community/embed" // the compressed front-end code and static data
	_ "github.com/go-sql-driver/mysql"      // the mysql driver is required behind the scenes
	"github.com/jmoiron/sqlx"
)

func init() {
	// runtime stores server/application level information
	runtime := env.Runtime{}

	// wire up logging implementation
	runtime.Log = logging.NewLogger()

	// define product edition details
	runtime.Product = env.ProdInfo{}
	runtime.Product.Major = "1"
	runtime.Product.Minor = "50"
	runtime.Product.Patch = "0"
	runtime.Product.Version = fmt.Sprintf("%s.%s.%s", runtime.Product.Major, runtime.Product.Minor, runtime.Product.Patch)
	runtime.Product.Edition = "Community"
	runtime.Product.Title = fmt.Sprintf("%s Edition", runtime.Product.Edition)
	runtime.Product.License = env.License{}
	runtime.Product.License.Seats = 1
	runtime.Product.License.Valid = true
	runtime.Product.License.Trial = false
	runtime.Product.License.Edition = "Community"

	runtime.Flags = env.ParseFlags()
	flagPrep(&runtime)

	// temp code repair
	api.Runtime = runtime
	request.Db = runtime.Db
}

func main() {

	// process the db value first
	// env.Parse("db")

	section.Register()

	ready := make(chan struct{}, 1) // channel is used for testing
	endpoint.Serve(ready)
}

func flagPrep(r *env.Runtime) bool {
	// Prepare DB
	db, err := sqlx.Open("mysql", stdConn(r.Flags.DBConn))
	if err != nil {
		r.Log.Error("unable to setup database", err)
	}

	r.Db = db
	r.Db.SetMaxIdleConns(30)
	r.Db.SetMaxOpenConns(100)
	r.Db.SetConnMaxLifetime(time.Second * 14400)

	err = r.Db.Ping()
	if err != nil {
		r.Log.Error("unable to connect to database, connection string should be of the form: '"+
			"username:password@tcp(host:3306)/database'", err)
		return false
	}

	// go into setup mode if required
	if r.Flags.SiteMode != web.SiteModeOffline {
		if database.Check(*r) {
			if err := database.Migrate(true /* the config table exists */); err != nil {
				r.Log.Error("unable to run database migration", err)
				return false
			}
		} else {
			r.Log.Info("going into setup mode to prepare new database")
		}
	}

	// Prepare SALT
	if r.Flags.Salt == "" {
		b := make([]byte, 17)

		_, err := rand.Read(b)
		if err != nil {
			r.Log.Error("problem using crypto/rand", err)
			return false
		}

		for k, v := range b {
			if (v >= 'a' && v <= 'z') || (v >= 'A' && v <= 'Z') || (v >= '0' && v <= '0') {
				b[k] = v
			} else {
				s := fmt.Sprintf("%x", v)
				b[k] = s[0]
			}
		}

		r.Flags.Salt = string(b)
		r.Log.Info("please set DOCUMIZESALT or use -salt with this value: " + r.Flags.Salt)
	}

	// Prepare HTTP ports
	if r.Flags.SSLCertFile == "" && r.Flags.SSLKeyFile == "" {
		if r.Flags.HTTPPort == "" {
			r.Flags.HTTPPort = "80"
		}
	} else {
		if r.Flags.HTTPPort == "" {
			r.Flags.HTTPPort = "443"
		}
	}

	return true
}

var stdParams = map[string]string{
	"charset":          "utf8",
	"parseTime":        "True",
	"maxAllowedPacket": "4194304", // 4194304 // 16777216 = 16MB
}

func stdConn(cs string) string {
	queryBits := strings.Split(cs, "?")
	ret := queryBits[0] + "?"
	retFirst := true
	if len(queryBits) == 2 {
		paramBits := strings.Split(queryBits[1], "&")
		for _, pb := range paramBits {
			found := false
			if assignBits := strings.Split(pb, "="); len(assignBits) == 2 {
				_, found = stdParams[strings.TrimSpace(assignBits[0])]
			}
			if !found { // if we can't work out what it is, put it through
				if retFirst {
					retFirst = false
				} else {
					ret += "&"
				}
				ret += pb
			}
		}
	}
	for k, v := range stdParams {
		if retFirst {
			retFirst = false
		} else {
			ret += "&"
		}
		ret += k + "=" + v
	}
	return ret
}
