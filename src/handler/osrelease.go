package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v9"
	"github.com/google/uuid"
)

type OsRelease struct {
	ID              uuid.UUID `json:"id"`               // /sys/devices/virtual/dmi/id/product_uuid
	OsId            string    `json:"os_id"`            // ID=debian
	VersionCodename string    `json:"version_codename"` // VERSION_CODENAME=bullseye
	Version         string    `json:"version"`          // VERSION="11 (bullseye)"
	VersionId       string    `json:"version_id"`       // VERSION_ID="11"
	DataCenter      string    `json:"data_center"`      // team or dc name
	HostIP          string    `json:"host_ip"`          // host ip
	UpdatedAt       string    `json:"updated_at"`       // unix timestamp
}

type OsReleases struct {
	Items map[uuid.UUID]OsRelease
}

var (
	ErrInsertFailed = errors.New("Insert failed")
	ErrMarshlFailed = errors.New("Marshal failed")
	ErrIDNotFound   = errors.New("Id not found")
	ErrNoKeysFound  = errors.New("No keys found")
)

func (c *OsReleases) Insert(rel OsRelease, ctx context.Context, con *redis.Client, ttl int) (uuid.UUID, error) {
	rel.ID = UUIDFromDcAndIP(rel.DataCenter, rel.HostIP)
	rel.UpdatedAt = fmt.Sprint(time.Now().Unix())
	srt, err := json.Marshal(rel)
	if err != nil {
		return rel.ID, ErrMarshlFailed
	}
	result, err := con.Set(ctx, fmt.Sprint(rel.ID), srt, time.Duration(ttl)*time.Second).Result()
	if err != nil {
		return rel.ID, ErrInsertFailed
	}
	log.Printf("Creating %s: %s", rel.ID, result)
	return rel.ID, nil
}

func (c *OsReleases) Retrieve(id uuid.UUID, ctx context.Context, con *redis.Client) (OsRelease, error) {
	result, err := con.Get(ctx, fmt.Sprint(id)).Result()
	if err != nil {
		return OsRelease{}, ErrIDNotFound
	}
	rel := OsRelease{}
	json.Unmarshal([]byte(result), &rel)
	return rel, nil
}

func (c *OsReleases) Scan(ctx context.Context, con *redis.Client) (OsReleases, error) {
	var rels = OsReleases{
		Items: make(map[uuid.UUID]OsRelease),
	}
	iter := con.Scan(ctx, 0, "*", 0).Iterator()
	for iter.Next(ctx) {
		uid, err := uuid.Parse(iter.Val())
		if err != nil {
			if iter.Val() != "eol_cache:all_packages" {
				log.Printf("Can't parse uuid: %s, %v", iter.Val(), err)
				continue
			} else {
				continue
			}
		} else {
			rels.Items[uid], _ = c.Retrieve(uid, ctx, con)
		}
	}
	if err := iter.Err(); err != nil {
		panic(err)
	}
	return rels, nil
}

func UUIDFromDcAndIP(dc string, ip string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(fmt.Sprintf("%s-%s", dc, ip)))
}
