package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const UUIDSuffix = "PACKAGE_UUID"

type PackageDetail struct {
	CurrentVersion    string `json:"current_version"`
	CurrentVersionEoF string `json:"current_version_eof"`
	NewestVersion     string `json:"newest_version"`
	Expired           bool   `json:"expired"`
}

type PackageVersions struct {
	IDPkg         uuid.UUID                `json:"id"`
	DataCenterPkg string                   `json:"data_center"`
	HostIPPkg     string                   `json:"host_ip"`
	Team          string                   `json:"team"`
	UpdatedAt     string                   `json:"updated_at"`
	Packages      map[string]PackageDetail `json:"packages"`
}

type PackageVersionss struct {
	Items map[uuid.UUID]PackageVersions
}

type EOL string

func (e *EOL) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*e = EOL(str)
		return nil
	}

	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if b {
			*e = EOL("true")
		} else {
			*e = EOL("false")
		}
		return nil
	}

	return fmt.Errorf("invalid EOL value")
}

type EndOfLifeEntry struct {
	Cycle             string `json:"cycle"`
	EOL               EOL    `json:"eol"`
	Latest            string `json:"latest"`
	LatestReleaseDate string `json:"latestReleaseDate"`
}

var (
	ErrInsertFailedPackage  = errors.New("Insert failed")
	ErrMarshalFailedPackage = errors.New("Marshal failed")
	ErrIDNotFoundPackage    = errors.New("ID not found")
)

func (c *PackageVersionss) Insert(
	pkg PackageVersions,
	ctx context.Context,
	con *redis.Client,
	queryFunc func(name, version string) (string, string, error),
	ttl int,
) (uuid.UUID, error) {

	updatedPackages := make(map[string]PackageDetail)

	for name, versionDetail := range pkg.Packages {
		if versionDetail.CurrentVersion == "unknown" || versionDetail.CurrentVersion == "" {
			continue
		}

		currentVersion := extractMajorMinor(versionDetail.CurrentVersion)
		latestVersion, eolDate, err := queryFunc(name, versionDetail.CurrentVersion)
		if err != nil {
			latestVersion = "unknown"
		} else {
			latestVersion = extractMajorMinor(latestVersion)
		}

		if eolDate == "" {
			eolDate = "false"
		}

		expired := isEOLReached(eolDate, time.Now())

		updatedPackages[name] = PackageDetail{
			CurrentVersion:    currentVersion,
			CurrentVersionEoF: eolDate,
			NewestVersion:     latestVersion,
			Expired:           expired,
		}
	}

	pkg.Packages = updatedPackages
	pkg.IDPkg = UUIDFromDcAndIPPackage(pkg.DataCenterPkg, pkg.HostIPPkg)
	pkg.UpdatedAt = fmt.Sprint(time.Now().Unix())

	data, err := json.Marshal(pkg)
	if err != nil {
		return pkg.IDPkg, ErrMarshalFailedPackage
	}

	var result string
	result, err = con.Set(ctx, fmt.Sprint(pkg.IDPkg), data, time.Duration(ttl)*time.Second).Result()
	if err != nil {
		return pkg.IDPkg, ErrInsertFailedPackage
	}
	log.Printf("Creating %s: %s", pkg.IDPkg, result)
	return pkg.IDPkg, nil
}

func (c *PackageVersionss) Retrieve(id uuid.UUID, ctx context.Context, con *redis.Client) (PackageVersions, error) {
	result, err := con.Get(ctx, fmt.Sprint(id)).Result()
	if err != nil {
		return PackageVersions{}, ErrIDNotFoundPackage
	}

	pkg := PackageVersions{}
	err = json.Unmarshal([]byte(result), &pkg)
	if err != nil {
		return PackageVersions{}, ErrMarshalFailedPackage
	}
	return pkg, nil
}

func (c *PackageVersionss) Scan(ctx context.Context, con *redis.Client) (PackageVersionss, error) {
	pkgs := PackageVersionss{
		Items: make(map[uuid.UUID]PackageVersions),
	}

	var uids []uuid.UUID
	var keys []string
	iter := con.Scan(ctx, 0, "*", 0).Iterator()
	for iter.Next(ctx) {
		uid, err := uuid.Parse(iter.Val())
		if err != nil {
			continue
		}
		uids = append(uids, uid)
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		log.Printf("Error scanning Redis keys: %v", err)
		return pkgs, err
	}
	if len(keys) == 0 {
		return pkgs, nil
	}

	values, err := con.MGet(ctx, keys...).Result()
	if err != nil {
		log.Printf("Error fetching Redis keys: %v", err)
		return pkgs, err
	}
	for i, val := range values {
		if val == nil {
			// Key expired between SCAN and MGET.
			continue
		}
		str, ok := val.(string)
		if !ok {
			log.Printf("Unexpected value type for key %s", keys[i])
			continue
		}
		var pkg PackageVersions
		if err := json.Unmarshal([]byte(str), &pkg); err != nil {
			log.Printf("Can't unmarshal package %s: %v", keys[i], err)
			continue
		}
		pkgs.Items[uids[i]] = pkg
	}

	return pkgs, nil
}

func UUIDFromDcAndIPPackage(dc string, ip string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(fmt.Sprintf("%s-%s-%s", dc, ip, UUIDSuffix)))
}
