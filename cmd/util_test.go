/*
 * Copyright 2018 The NATS Authors
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/nats-io/jwt"
	"github.com/nats-io/nkeys"
	"github.com/nats-io/nsc/cli"
	"github.com/nats-io/nsc/cmd/store"
	"github.com/stretchr/testify/require"
)

type TestStore struct {
	Dir string

	Store    *store.Store
	KeyStore store.KeyStore

	OperatorKey     nkeys.KeyPair
	OperatorKeyPath string
}

func NewTestStoreWithOperator(t *testing.T, operatorName string, operator nkeys.KeyPair) *TestStore {
	var ts TestStore
	var err error

	// ngsStore is a global - so first test to get it initializes it
	ngsStore = nil

	ts.OperatorKey = operator

	ts.Dir = MakeTempDir(t)
	// debug the test that created the store
	_ = ioutil.WriteFile(filepath.Join(ts.Dir, "test.txt"), []byte(t.Name()), 0700)
	storeRoot := filepath.Join(ts.Dir, "store")
	operatorRoot := filepath.Join(storeRoot, operatorName)
	err = os.MkdirAll(operatorRoot, 0700)
	require.NoError(t, err, "error creating %q", operatorRoot)

	nkeysDir := filepath.Join(ts.Dir, "keys")
	err = os.Mkdir(nkeysDir, 0700)
	require.NoError(t, err, "error creating %q", nkeysDir)
	err = os.Setenv(store.NKeysPathEnv, nkeysDir)
	require.NoError(t, err, "nkeys env")

	var nk = &store.NamedKey{}
	nk.Name = operatorName
	if ts.OperatorKey != nil {
		nk.KP = ts.OperatorKey
	}

	SetStoreRoot(storeRoot)
	SetOperator(operatorName)
	ts.Store, err = store.CreateStore(operatorName, storeRoot, nk)
	ctx, err := ts.Store.GetContext()
	require.NoError(t, err, "getting context")

	ts.KeyStore = ctx.KeyStore
	if ts.OperatorKey != nil {
		ts.OperatorKeyPath, err = ts.KeyStore.Store(operatorName, ts.OperatorKey, "")
		require.NoError(t, err, "store operator key")
	}

	return &ts
}

func NewTestStore(t *testing.T, operatorName string) *TestStore {
	_, _, kp := CreateOperatorKey(t)
	return NewTestStoreWithOperator(t, operatorName, kp)
}

func TestStoreTree(t *testing.T) {
	ts := NewTestStore(t, "foo")
	t.Log(ts.Dir)
	t.Log("operatorName", GetConfig().Operator)

	ts.AddAccount(t, "bar")
	ts.AddAccount(t, "foo")

	v, err := store.LoadStore(filepath.Join(config.StoreRoot, config.Operator))
	require.NoError(t, err)
	require.NotNil(t, v)
}

func (ts *TestStore) Done(t *testing.T) {
	cli.ResetPromptLib()
	if t.Failed() {
		t.Log("test artifacts:", ts.Dir)
	}
}

func (ts *TestStore) AddAccount(t *testing.T, accountName string) {
	if !ts.Store.Has(store.Accounts, accountName, store.JwtName(accountName)) {
		_, _, err := ExecuteCmd(CreateAddAccountCmd(), "--name", accountName)
		require.NoError(t, err, "account creation")
	}
}

func (ts *TestStore) AddUser(t *testing.T, accountName string, userName string) {
	ts.AddAccount(t, accountName)
	_, _, err := ExecuteCmd(CreateAddUserCmd(), "--account", accountName, "--name", userName)
	require.NoError(t, err, "user creation")
}

func (ts *TestStore) AddCluster(t *testing.T, clusterName string) {
	if !ts.Store.Has(store.Clusters, clusterName, store.JwtName(clusterName)) {
		_, _, err := ExecuteCmd(createAddClusterCmd(), "--name", clusterName)
		require.NoError(t, err, "cluster creation")
	}
}

func (ts *TestStore) AddServer(t *testing.T, clusterName string, serverName string) {
	ts.AddCluster(t, clusterName)
	_, _, err := ExecuteCmd(createAddServerCmd(), "--cluster", clusterName, "--name", serverName)
	require.NoError(t, err, "server creation")
}

func (ts *TestStore) AddExport(t *testing.T, accountName string, kind jwt.ExportType, subject string, public bool) {
	flags := []string{"--account", accountName, "--subject", subject}
	if !public {
		flags = append(flags, "--private")
	}
	if kind == jwt.Service {
		flags = append(flags, "--service")
	}

	ts.AddAccount(t, accountName)
	_, _, err := ExecuteCmd(createAddExportCmd(), flags...)
	require.NoError(t, err)
}

func (ts *TestStore) AddImport(t *testing.T, srcAccount string, subject string, targetAccountName string) {
	token := ts.GenerateActivation(t, srcAccount, subject, targetAccountName)

	f, err := ioutil.TempFile(ts.Dir, "token")
	require.NoError(t, err)
	_, err = f.WriteString(token)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	flags := []string{"--account", targetAccountName, "--token", f.Name()}
	_, _, err = ExecuteCmd(createAddImportCmd(), flags...)
	require.NoError(t, err)
}

func (ts *TestStore) GenerateActivation(t *testing.T, srcAccount string, subject string, targetAccount string) string {
	tkp, err := ts.KeyStore.GetAccountKey(targetAccount)
	require.NoError(t, err)
	tpub, err := tkp.PublicKey()
	require.NoError(t, err)

	flags := []string{"--account", srcAccount, "--target-account", tpub, "--subject", subject}
	stdout, _, err := ExecuteCmd(createGenerateActivationCmd(), flags...)
	require.NoError(t, err)
	return ExtractToken(stdout)
}

func MakeTempDir(t *testing.T) string {
	p, err := ioutil.TempDir("", "store_test")
	require.NoError(t, err)
	return p
}

func StoreKey(t *testing.T, kp nkeys.KeyPair, dir string) string {
	p, err := kp.PublicKey()
	require.NoError(t, err)

	s, err := kp.Seed()
	require.NoError(t, err)

	fp := filepath.Join(dir, string(p)+".nk")
	err = ioutil.WriteFile(fp, s, 0600)
	require.NoError(t, err)
	return fp
}

func CreateClusterKey(t *testing.T) (seed []byte, pub string, kp nkeys.KeyPair) {
	return CreateNkey(t, nkeys.PrefixByteCluster)
}

func CreateServerKey(t *testing.T) (seed []byte, pub string, kp nkeys.KeyPair) {
	return CreateNkey(t, nkeys.PrefixByteServer)
}

func CreateAccountKey(t *testing.T) (seed []byte, pub string, kp nkeys.KeyPair) {
	return CreateNkey(t, nkeys.PrefixByteAccount)
}

func CreateUserKey(t *testing.T) (seed []byte, pub string, kp nkeys.KeyPair) {
	return CreateNkey(t, nkeys.PrefixByteUser)
}

func CreateOperatorKey(t *testing.T) (seed []byte, pub string, kp nkeys.KeyPair) {
	return CreateNkey(t, nkeys.PrefixByteOperator)
}

func CreateNkey(t *testing.T, kind nkeys.PrefixByte) ([]byte, string, nkeys.KeyPair) {
	kp, err := nkeys.CreatePair(kind)
	require.NoError(t, err)

	seed, err := kp.Seed()
	require.NoError(t, err)

	pub, err := kp.PublicKey()
	require.NoError(t, err)

	return seed, pub, kp
}
