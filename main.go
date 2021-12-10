package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tm "github.com/buger/goterm"
	"github.com/olekukonko/tablewriter"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
)

type LeaseKeysMapping map[int64]LeaseInfo

func (m LeaseKeysMapping) keys() []int64 {
	keys := make([]int64, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func (m LeaseKeysMapping) keysSortedByLeaseID() []int64 {
	keys := m.keys()
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}

// TODO:
// func (m LeaseKeysMapping) keysSortedAttachedAndPrevAttached() []int64 {
// 	keys := m.keys()
// 	sort.Slice(keys, func(i, j int) bool {
// 		return keys[i] < keys[j]
// 	})
// 	return keys
// }

type LeaseInfo struct {
	ID               int64
	AttachedKeys     []string
	PrevAttachedKeys []string
	GrantedTTL       int64
	TTL              int64
}

func (l LeaseInfo) HexID() string {
	return fmt.Sprintf("%016x", l.ID)
}

func (l LeaseInfo) generateTableRow() []string {
	return []string{
		l.HexID(),
		strconv.FormatInt(l.GrantedTTL, 10),
		strconv.FormatInt(l.TTL, 10),
		strings.Join(l.AttachedKeys, ", "),
		strings.Join(l.PrevAttachedKeys, ", "),
	}
}

func main() {
	ctx := context.Background()

	clientURLs := []string{"http://localhost:2379"}

	clientv3Config := clientv3.Config{
		Endpoints:   clientURLs,
		DialTimeout: 5 * time.Second,
		DialOptions: []grpc.DialOption{
			grpc.WithReturnConnectionError(),
			grpc.WithBlock(),
		},
	}
	client, err := clientv3.New(clientv3Config)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	if _, err := client.Get(ctx, "/sensu.io"); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	leaseKeysMapping := make(LeaseKeysMapping)

	table := tablewriter.NewWriter(tm.Output)
	table.SetHeader([]string{"Lease ID", "Granted", "TTL", "Attached", "Prev Attached"})
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetTablePadding(" ")
	table.SetNoWhiteSpace(true)
	table.SetBorder(false)

	for {
		table.ClearRows()
		tm.MoveCursor(1, 1)
		tm.Clear()
		tm.Flush()

		leaseResp, err := client.Leases(ctx)
		if err != nil {
			tm.Println(err)
			tm.Flush()
			os.Exit(1)
		}

		// remove leases that no longer exist from map and reset attached status
		// to false
		for leaseKey := range leaseKeysMapping {
			deleteKey := true

			for _, lease := range leaseResp.Leases {
				if leaseKey == int64(lease.ID) {
					deleteKey = false
					break
				}
			}

			if deleteKey {
				delete(leaseKeysMapping, leaseKey)
			}
		}

		for _, lease := range leaseResp.Leases {
			leaseID := int64(lease.ID)
			if leaseInfo, ok := leaseKeysMapping[leaseID]; !ok {
				leaseInfo.ID = leaseID
				leaseInfo.AttachedKeys = []string{}
				leaseInfo.PrevAttachedKeys = []string{}
				leaseInfo.TTL = 0
				leaseKeysMapping[leaseID] = leaseInfo
			}

			opts := []clientv3.LeaseOption{clientv3.WithAttachedKeys()}
			ttlResp, err := client.TimeToLive(ctx, lease.ID, opts...)
			if err != nil {
				tm.Println(err)
				tm.Flush()
				os.Exit(1)
			}

			if leaseInfo, ok := leaseKeysMapping[leaseID]; ok {
				for _, keyName := range leaseInfo.AttachedKeys {
					attachedKey := false

					for _, keyBytes := range ttlResp.Keys {
						if keyName == string(keyBytes) {
							attachedKey = true
							break
						}
					}

					if !attachedKey {
						leaseInfo.PrevAttachedKeys = append(leaseInfo.PrevAttachedKeys, keyName)
					}
				}
				leaseInfo.AttachedKeys = []string{}
				leaseKeysMapping[leaseID] = leaseInfo
			}

			// add currently attached keys to mapping
			for _, keyBytes := range ttlResp.Keys {
				key := string(keyBytes)

				if leaseInfo, ok := leaseKeysMapping[leaseID]; ok {
					leaseInfo.GrantedTTL = ttlResp.GrantedTTL
					leaseInfo.TTL = ttlResp.TTL
					leaseInfo.AttachedKeys = append(leaseInfo.AttachedKeys, key)
					leaseKeysMapping[leaseID] = leaseInfo
				}
			}
		}

		table.SetColumnColor(tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
			tablewriter.Colors{},
			tablewriter.Colors{},
			tablewriter.Colors{tablewriter.FgGreenColor},
			tablewriter.Colors{tablewriter.FgRedColor})

		for _, lid := range leaseKeysMapping.keysSortedByLeaseID() {
			if leaseInfo, ok := leaseKeysMapping[lid]; ok {
				table.Append(leaseInfo.generateTableRow())
			}
		}

		table.Render()
		tm.Flush()
		time.Sleep(1 * time.Second)
	}
}
