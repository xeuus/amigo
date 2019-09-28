package cmd

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func getEnv(key, def string) string {
	op := os.Getenv(key)
	if op == "" {
		return def
	}
	return op
}

var (
	DBDriver = getEnv("DB_DRIVER", "mysql")
	DBQuery  = getEnv("DB_QUERY", "")
)
var db *sql.DB

func configSql() {
	var err error
	db, err = sql.Open(DBDriver, DBQuery)
	if err != nil {
		log.Fatal(err)
	}
}

func readFile(name string) (string, string) {
	file, err := os.Open(name)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var up, down string
	flg := 0
	for scanner.Scan() {
		txt := strings.Trim(scanner.Text(), " \n")
		if strings.Contains(txt, "migrate_up") {
			flg = 0
		} else if strings.Contains(txt, "migrate_down") {
			flg = 1
		} else {
			if flg == 0 {
				up += txt + "\n"
			} else if flg == 1 {
				down += txt + "\n"
			}
		}
	}
	up = strings.Trim(up, " \n")
	down = strings.Trim(down, " \n")
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	return up, down
}

func Init() {
	path := flag.String("path", "migrations", "migrations path relative to current directory")
	flag.Parse()
	action := flag.Arg(0)
	if action == "" {
		action = "create"
	}
	switch action {
	case "create":
		propertyName := flag.Arg(1)
		if propertyName == "" {
			propertyName = "some"
		}
		propertyName = dashify(propertyName)
		isTable := strings.Contains(propertyName, "table")

		var data []byte
		if isTable {
			data = []byte(`/* -- migrate_up -- */
create table TABLE_NAME(
	id int auto_increment,
	constraint primary key (id)
);
/* -- migrate_down -- */
drop table TABLE_NAME;`)
		} else  {
			data = []byte(`/* -- migrate_up -- */
/* -- migrate_down -- */`)
		}
		_ = ioutil.WriteFile(
			fmt.Sprintf("%s/%s_create_%s.sql", *path, time.Now().UTC().Format("2006_01_02_15_04_05"), propertyName),
			data,
			0644,
		)
		return
	case "up":
		configSql()
		createMigrationTable()

		saved := retrieveMigratedList()
		files, err := ioutil.ReadDir(*path)
		if err != nil {
			log.Fatal(err)
		}
		var names []string
		for _, f := range files {
			if !f.IsDir() {
				names = append(names, f.Name())
			}
		}
		sort.Strings(names)
		savedLen := len(saved)

		tx, _ := db.Begin()
		for i, name := range names {
			if i < savedLen && saved[i] == name {
				log.Println("> already migrated: ", name)
			}else {
				up, _ := readFile(*path + "/" + name)
				if err := exec(up); err != nil {
					tx.Rollback()
					log.Fatal(err)
					return
				}
				if err := addMigration(name, i); err != nil {
					tx.Rollback()
					log.Fatal(err)
					return
				}
				log.Println(">> succeed : ", name)
			}
		}
		tx.Commit()
		return
	case "down":
		configSql()
		createMigrationTable()
		saved := retrieveMigratedList()
		savedLen := len(saved)
		tx, _ := db.Begin()
		for i := savedLen -1; i >= 0; i-- {
			name := saved[i]
			_, down := readFile(*path + "/" + name)
			if err := exec(down); err != nil {
				tx.Rollback()
				log.Fatal(err)
				return
			}
			if err := removeMigration(i); err != nil {
				tx.Rollback()
				log.Fatal(err)
				return
			}
			log.Println(">> rolled-back : ", name)
		}
		tx.Commit()
		return
	case "rollback":
		stepsArg := flag.Arg(0)
		if stepsArg == "" {
			stepsArg = "1"
		}
		steps, _ := strconv.Atoi(stepsArg)
		configSql()
		createMigrationTable()
		saved := retrieveMigratedList()
		savedLen := len(saved)
		k := 0
		tx, _ := db.Begin()
		for i := savedLen -1; i >= 0; i-- {
			name := saved[i]
			_, down := readFile(*path + "/" + name)
			if err := exec(down); err != nil {
				tx.Rollback()
				log.Fatal(err)
				return
			}
			if err := removeMigration(i); err != nil {
				tx.Rollback()
				log.Fatal(err)
				return
			}
			log.Println(">> rolled-back : ", name)
			k ++
			if k == steps {
				break
			}
		}
		tx.Commit()
		return
	}
}

func dashify(in string) string {
	return strings.ReplaceAll(strings.ToLower(in), " ", "_")
}

func createMigrationTable() {
	_, err := db.Exec(`create table if not exists amigo_migrations (
	id int not null auto_increment,
	name varchar(255) not null,
	priority int not null,
	created_at timestamp not null default current_timestamp,
	constraint amigo_migrations_id_pk primary key (id),
	constraint amigo_migrations_name_uq unique (name)
);`)
	if err != nil {
		log.Fatal(err)
	}
}

func retrieveMigratedList() []string {
	var names []string
	rows, err := db.Query(`select name from amigo_migrations order by priority;`)
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		var name string
		err := rows.Scan(&name)
		if err != nil {
			log.Fatal(err)
		}
		names = append(names, name)
	}
	return names
}

func addMigration(name string, priority int) error {
	_, err := db.Exec(`insert into amigo_migrations (name, priority) values (?, ?);`, name, priority)
	if err != nil {
		return err
	}
	return nil
}
func removeMigration(priority int) error {
	_, err := db.Exec(`delete from amigo_migrations where priority=?;`, priority)
	if err != nil {
		return err
	}
	return nil
}

func exec(query string, args ...interface{}) error {
	_, err := db.Exec(query, args...)
	if err != nil {
		return err
	}
	return nil
}
