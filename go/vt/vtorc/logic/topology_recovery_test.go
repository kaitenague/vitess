/*
Copyright 2022 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package logic

import (
	"context"
	"testing"

	"vitess.io/vitess/go/vt/log"

	"github.com/stretchr/testify/require"

	topodatapb "vitess.io/vitess/go/vt/proto/topodata"
	"vitess.io/vitess/go/vt/topo/memorytopo"
	"vitess.io/vitess/go/vt/topo/topoproto"
	"vitess.io/vitess/go/vt/vtorc/config"
	"vitess.io/vitess/go/vt/vtorc/db"
	"vitess.io/vitess/go/vt/vtorc/inst"
	_ "vitess.io/vitess/go/vt/vttablet/grpctmclient"
)

func TestAnalysisEntriesHaveSameRecovery(t *testing.T) {
	tests := []struct {
		prevAnalysisCode inst.AnalysisCode
		newAnalysisCode  inst.AnalysisCode
		shouldBeEqual    bool
	}{
		{
			// DeadPrimary and DeadPrimaryAndSomeReplicas have the same recovery
			prevAnalysisCode: inst.DeadPrimary,
			newAnalysisCode:  inst.DeadPrimaryAndSomeReplicas,
			shouldBeEqual:    true,
		}, {
			// DeadPrimary and StalledDiskPrimary have the same recovery
			prevAnalysisCode: inst.DeadPrimary,
			newAnalysisCode:  inst.PrimaryDiskStalled,
			shouldBeEqual:    true,
		}, {
			// PrimarySemiSyncBlocked and PrimaryDiskStalled have the same recovery
			prevAnalysisCode: inst.PrimarySemiSyncBlocked,
			newAnalysisCode:  inst.PrimaryDiskStalled,
			shouldBeEqual:    true,
		}, {
			// DeadPrimary and PrimaryTabletDeleted are different recoveries.
			prevAnalysisCode: inst.DeadPrimary,
			newAnalysisCode:  inst.PrimaryTabletDeleted,
			shouldBeEqual:    false,
		}, {
			// same codes will always have same recovery
			prevAnalysisCode: inst.DeadPrimary,
			newAnalysisCode:  inst.DeadPrimary,
			shouldBeEqual:    true,
		}, {
			prevAnalysisCode: inst.PrimaryHasPrimary,
			newAnalysisCode:  inst.DeadPrimaryAndSomeReplicas,
			shouldBeEqual:    false,
		}, {
			prevAnalysisCode: inst.DeadPrimary,
			newAnalysisCode:  inst.PrimaryHasPrimary,
			shouldBeEqual:    false,
		}, {
			prevAnalysisCode: inst.LockedSemiSyncPrimary,
			newAnalysisCode:  inst.PrimaryHasPrimary,
			shouldBeEqual:    false,
		}, {
			prevAnalysisCode: inst.PrimaryIsReadOnly,
			newAnalysisCode:  inst.PrimarySemiSyncMustNotBeSet,
			shouldBeEqual:    true,
		}, {
			prevAnalysisCode: inst.PrimarySemiSyncMustBeSet,
			newAnalysisCode:  inst.PrimarySemiSyncMustNotBeSet,
			shouldBeEqual:    true,
		}, {
			prevAnalysisCode: inst.PrimaryCurrentTypeMismatch,
			newAnalysisCode:  inst.PrimarySemiSyncMustNotBeSet,
			shouldBeEqual:    true,
		}, {
			prevAnalysisCode: inst.PrimaryIsReadOnly,
			newAnalysisCode:  inst.DeadPrimary,
			shouldBeEqual:    false,
		}, {
			prevAnalysisCode: inst.NotConnectedToPrimary,
			newAnalysisCode:  inst.ConnectedToWrongPrimary,
			shouldBeEqual:    true,
		}, {
			prevAnalysisCode: inst.ConnectedToWrongPrimary,
			newAnalysisCode:  inst.ReplicaIsWritable,
			shouldBeEqual:    true,
		},
	}
	t.Parallel()
	for _, tt := range tests {
		t.Run(string(tt.prevAnalysisCode)+","+string(tt.newAnalysisCode), func(t *testing.T) {
			res := analysisEntriesHaveSameRecovery(&inst.ReplicationAnalysis{Analysis: tt.prevAnalysisCode}, &inst.ReplicationAnalysis{Analysis: tt.newAnalysisCode})
			require.Equal(t, tt.shouldBeEqual, res)
		})
	}
}

func TestElectNewPrimaryPanic(t *testing.T) {
	orcDb, err := db.OpenVTOrc()
	require.NoError(t, err)
	oldTs := ts
	defer func() {
		ts = oldTs
		_, err = orcDb.Exec("delete from vitess_tablet")
		require.NoError(t, err)
	}()

	tablet := &topodatapb.Tablet{
		Alias: &topodatapb.TabletAlias{
			Cell: "zone1",
			Uid:  100,
		},
		Hostname:      "localhost",
		MysqlHostname: "localhost",
		MysqlPort:     1200,
		Keyspace:      "ks",
		Shard:         "-",
		Type:          topodatapb.TabletType_REPLICA,
	}
	err = inst.SaveTablet(tablet)
	require.NoError(t, err)
	analysisEntry := &inst.ReplicationAnalysis{
		AnalyzedInstanceAlias: topoproto.TabletAliasString(tablet.Alias),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ts = memorytopo.NewServer(ctx, "zone1")
	recoveryAttempted, _, err := electNewPrimary(context.Background(), analysisEntry, log.NewPrefixedLogger("prefix"))
	require.True(t, recoveryAttempted)
	require.Error(t, err)
}

func TestRecoveryRegistration(t *testing.T) {
	orcDb, err := db.OpenVTOrc()
	require.NoError(t, err)
	oldTs := ts
	defer func() {
		ts = oldTs
		_, err = orcDb.Exec("delete from vitess_tablet")
		require.NoError(t, err)
	}()

	primary := &topodatapb.Tablet{
		Alias: &topodatapb.TabletAlias{
			Cell: "zone1",
			Uid:  1,
		},
		Hostname:      "localhost1",
		MysqlHostname: "localhost1",
		MysqlPort:     1200,
		Keyspace:      "ks",
		Shard:         "0",
		Type:          topodatapb.TabletType_PRIMARY,
	}
	replica := &topodatapb.Tablet{
		Alias: &topodatapb.TabletAlias{
			Cell: "zone1",
			Uid:  2,
		},
		Hostname:      "localhost2",
		MysqlHostname: "localhost2",
		MysqlPort:     1200,
		Keyspace:      "ks",
		Shard:         "0",
		Type:          topodatapb.TabletType_REPLICA,
	}
	err = inst.SaveTablet(primary)
	require.NoError(t, err)
	err = inst.SaveTablet(replica)
	require.NoError(t, err)
	primaryAnalysisEntry := inst.ReplicationAnalysis{
		AnalyzedInstanceAlias: topoproto.TabletAliasString(primary.Alias),
		Analysis:              inst.ReplicationStopped,
	}
	replicaAnalysisEntry := inst.ReplicationAnalysis{
		AnalyzedInstanceAlias: topoproto.TabletAliasString(replica.Alias),
		Analysis:              inst.DeadPrimary,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ts = memorytopo.NewServer(ctx, "zone1")
	tp, err := AttemptRecoveryRegistration(&replicaAnalysisEntry)
	require.NoError(t, err)

	// because there is another recovery in progress for this shard, this will fail.
	_, err = AttemptRecoveryRegistration(&primaryAnalysisEntry)
	require.ErrorContains(t, err, "Active recovery")

	// Lets say the recovery finishes after some time.
	err = resolveRecovery(tp, nil)
	require.NoError(t, err)

	// now this recovery registration should be successful.
	_, err = AttemptRecoveryRegistration(&primaryAnalysisEntry)
	require.NoError(t, err)
}

func TestGetCheckAndRecoverFunctionCode(t *testing.T) {
	tests := []struct {
		name                         string
		ersEnabled                   bool
		convertTabletWithErrantGTIDs bool
		analysisCode                 inst.AnalysisCode
		wantRecoveryFunction         recoveryFunction
	}{
		{
			name:                 "DeadPrimary with ERS enabled",
			ersEnabled:           true,
			analysisCode:         inst.DeadPrimary,
			wantRecoveryFunction: recoverDeadPrimaryFunc,
		}, {
			name:                 "DeadPrimary with ERS disabled",
			ersEnabled:           false,
			analysisCode:         inst.DeadPrimary,
			wantRecoveryFunction: noRecoveryFunc,
		}, {
			name:                 "StalledDiskPrimary with ERS enabled",
			ersEnabled:           true,
			analysisCode:         inst.PrimaryDiskStalled,
			wantRecoveryFunction: recoverDeadPrimaryFunc,
		}, {
			name:                 "StalledDiskPrimary with ERS disabled",
			ersEnabled:           false,
			analysisCode:         inst.PrimaryDiskStalled,
			wantRecoveryFunction: noRecoveryFunc,
		}, {
			name:                 "PrimarySemiSyncBlocked with ERS enabled",
			ersEnabled:           true,
			analysisCode:         inst.PrimarySemiSyncBlocked,
			wantRecoveryFunction: recoverDeadPrimaryFunc,
		}, {
			name:                 "PrimarySemiSyncBlocked with ERS disabled",
			ersEnabled:           false,
			analysisCode:         inst.PrimarySemiSyncBlocked,
			wantRecoveryFunction: noRecoveryFunc,
		}, {
			name:                 "PrimaryTabletDeleted with ERS enabled",
			ersEnabled:           true,
			analysisCode:         inst.PrimaryTabletDeleted,
			wantRecoveryFunction: recoverPrimaryTabletDeletedFunc,
		}, {
			name:                 "PrimaryTabletDeleted with ERS disabled",
			ersEnabled:           false,
			analysisCode:         inst.PrimaryTabletDeleted,
			wantRecoveryFunction: noRecoveryFunc,
		}, {
			name:                 "PrimaryHasPrimary",
			ersEnabled:           false,
			analysisCode:         inst.PrimaryHasPrimary,
			wantRecoveryFunction: recoverPrimaryHasPrimaryFunc,
		}, {
			name:                 "ClusterHasNoPrimary",
			ersEnabled:           false,
			analysisCode:         inst.ClusterHasNoPrimary,
			wantRecoveryFunction: electNewPrimaryFunc,
		}, {
			name:                 "ReplicationStopped",
			ersEnabled:           false,
			analysisCode:         inst.ReplicationStopped,
			wantRecoveryFunction: fixReplicaFunc,
		}, {
			name:                 "PrimarySemiSyncMustBeSet",
			ersEnabled:           false,
			analysisCode:         inst.PrimarySemiSyncMustBeSet,
			wantRecoveryFunction: fixPrimaryFunc,
		}, {
			name:                         "ErrantGTIDDetected",
			ersEnabled:                   false,
			convertTabletWithErrantGTIDs: true,
			analysisCode:                 inst.ErrantGTIDDetected,
			wantRecoveryFunction:         recoverErrantGTIDDetectedFunc,
		}, {
			name:                         "ErrantGTIDDetected with --change-tablets-with-errant-gtid-to-drained false",
			ersEnabled:                   false,
			convertTabletWithErrantGTIDs: false,
			analysisCode:                 inst.ErrantGTIDDetected,
			wantRecoveryFunction:         noRecoveryFunc,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prevVal := config.ERSEnabled()
			config.SetERSEnabled(tt.ersEnabled)
			defer config.SetERSEnabled(prevVal)

			convertErrantVal := config.ConvertTabletWithErrantGTIDs()
			config.SetConvertTabletWithErrantGTIDs(tt.convertTabletWithErrantGTIDs)
			defer config.SetConvertTabletWithErrantGTIDs(convertErrantVal)

			gotFunc := getCheckAndRecoverFunctionCode(tt.analysisCode, "")
			require.EqualValues(t, tt.wantRecoveryFunction, gotFunc)
		})
	}
}
