package cronjob

import (
	"encoding/json"
	"github.com/boltdb/bolt"
	"github.com/ego008/goyoubbs/model"
	"github.com/ego008/goyoubbs/system"
	"github.com/ego008/youdb"
	"github.com/weint/httpclient"
	"os"
	"strings"
	"time"
)

type BaseHandler struct {
	App *system.Application
}

func (h *BaseHandler) MainCronJob() {
	db := h.App.Db
	scf := h.App.Cf.Site
	tick1 := time.Tick(3600 * time.Second)
	tick2 := time.Tick(120 * time.Second)
	tick3 := time.Tick(30 * time.Minute)
	daySecond := int64(3600 * 24)

	for {
		select {
		case <-tick1:
			limit := 10
			timeBefore := uint64(time.Now().UTC().Unix() - daySecond)
			scoreStartB := youdb.I2b(timeBefore)
			zbnList := []string{
				"article_detail_token",
				"user_login_token",
			}
			for _, bn := range zbnList {
				rs := db.Zrscan(bn, []byte(""), scoreStartB, limit)
				if rs.State == "ok" {
					keys := make([][]byte, len(rs.Data)/2)
					j := 0
					for i := 0; i < (len(rs.Data) - 1); i += 2 {
						keys[j] = rs.Data[i]
						j++
					}
					db.Zmdel(bn, keys)
				}
			}

		case <-tick2:
			if scf.AutoGetTag && len(scf.GetTagApi) > 0 {
				getTagFromTitle(db, scf.GetTagApi)
			}
		case <-tick3:
			if h.App.Cf.Site.AutoDataBackup {
				dataBackup(db)
			}
		}
	}
}

func dataBackup(db *youdb.DB) {
	filePath := "databackup/" + time.Now().UTC().Format("2006-01-02") + ".db"
	if _, err := os.Stat(filePath); err != nil {
		// path not exists
		err := db.View(func(tx *bolt.Tx) error {
			return tx.CopyFile(filePath, 0600)
		})
		if err == nil {
			// todo upload to qiniu
		}
	}
}

func getTagFromTitle(db *youdb.DB, apiUrl string) {
	rs := db.Hscan("task_to_get_tag", []byte(""), 1)
	if rs.State == "ok" {
		aidB := rs.Data[0][:]

		rs2 := db.Hget("article", aidB)
		if rs2.State != "ok" {
			db.Hdel("task_to_get_tag", aidB)
			return
		}
		aobj := model.Article{}
		json.Unmarshal(rs2.Data[0], &aobj)
		if len(aobj.Tags) > 0 {
			db.Hdel("task_to_get_tag", aidB)
			return
		}

		hc := httpclient.NewHttpClientRequest("POST", apiUrl)
		hc.Param("state", "ok")
		hc.Param("ms", string(rs.Data[1]))

		t := struct {
			Code int    `json:"code"`
			Tag  string `json:"tag"`
		}{}
		err := hc.ReplyJson(&t)
		if err != nil {
			return
		}
		if hc.Status() == 200 && t.Code == 200 {
			if len(t.Tag) > 0 {
				tags := strings.Split(t.Tag, ",")
				if len(tags) > 5 {
					tags = tags[:5]
				}
				for _, tag := range tags {
					tagLow := strings.ToLower(tag)
					if db.Hget("tag", []byte(tagLow)).State != "ok" {
						db.Hset("tag", []byte(tagLow), []byte(""))
						db.HnextSequence("tag")
						aidB = youdb.I2b(aobj.Id) // fixed aidB
					}
					// check if not exist !important
					if db.Hget("tag:"+tagLow, aidB).State != "ok" {
						db.Hset("tag:"+tagLow, aidB, []byte(""))
						db.Zincr("tag_article_num", []byte(tagLow), 1)
						aidB = youdb.I2b(aobj.Id) // fixed aidB
					}
				}

				// get once more
				rs2 := db.Hget("article", aidB)
				if rs2.State == "ok" {
					aobj := model.Article{}
					json.Unmarshal(rs2.Data[0], &aobj)
					aobj.Tags = strings.Join(tags, ",")
					jb, _ := json.Marshal(aobj)
					db.Hset("article", aidB, jb)
				}
			}
			db.Hdel("task_to_get_tag", aidB)
		}
	}
}
