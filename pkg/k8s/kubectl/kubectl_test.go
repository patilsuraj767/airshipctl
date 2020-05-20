/*
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     https://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package kubectl_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"opendev.org/airship/airshipctl/pkg/document"
	"opendev.org/airship/airshipctl/pkg/k8s/kubectl"
	k8sutils "opendev.org/airship/airshipctl/pkg/k8s/utils"
	"opendev.org/airship/airshipctl/testutil"
	k8stest "opendev.org/airship/airshipctl/testutil/k8sutils"
)

var (
	kubeconfigPath = "testdata/kubeconfig.yaml"
	fixtureDir     = "testdata/"

	ErrWriteOutError = errors.New("ErrWriteOutError")
	ErrTempFileError = errors.New("ErrTempFileError")
)

type MockFileSystem struct {
	MockRemoveAll func() error
	MockTempFile  func() (document.File, error)
	document.FileSystem
}

func (fsys MockFileSystem) RemoveAll(string) error { return fsys.MockRemoveAll() }
func (fsys MockFileSystem) TempFile(string, string) (document.File, error) {
	return fsys.MockTempFile()
}

type TestFile struct {
	document.File
	MockName  func() string
	MockWrite func() (int, error)
	MockClose func() error
}

func (f TestFile) Name() string              { return f.MockName() }
func (f TestFile) Write([]byte) (int, error) { return f.MockWrite() }
func (f TestFile) Close() error              { return f.MockClose() }

func TestNewKubectlFromKubeConfigPath(t *testing.T) {
	f := k8sutils.FactoryFromKubeConfigPath(kubeconfigPath)
	kctl := kubectl.NewKubectl(f).WithBufferDir("/tmp/.airship")

	assert.NotNil(t, kctl.Factory)
	assert.NotNil(t, kctl.FileSystem)
	assert.NotNil(t, kctl.IOStreams)
}

func TestApply(t *testing.T) {
	f := k8stest.NewFakeFactoryForRC(t, filenameRC)
	defer f.Cleanup()
	kctl := kubectl.NewKubectl(f).WithBufferDir("/tmp/.airship")
	kctl.Factory = f
	ao, err := kctl.ApplyOptions()
	require.NoError(t, err, "failed to get documents from bundle")
	ao.SetDryRun(true)

	b := testutil.NewTestBundle(t, fixtureDir)
	docs, err := b.GetByAnnotation("airshipit.org/initinfra")
	require.NoError(t, err, "failed to get documents from bundle")

	tests := []struct {
		name        string
		expectedErr error
		fs          document.FileSystem
	}{
		{
			expectedErr: nil,
			fs: MockFileSystem{
				MockRemoveAll: func() error { return nil },
				MockTempFile: func() (document.File, error) {
					return TestFile{
						MockName:  func() string { return filenameRC },
						MockWrite: func() (int, error) { return 0, nil },
						MockClose: func() error { return nil },
					}, nil
				},
			},
		},
		{
			expectedErr: ErrWriteOutError,
			fs: MockFileSystem{
				MockTempFile: func() (document.File, error) { return nil, ErrWriteOutError }},
		},
		{
			expectedErr: ErrTempFileError,
			fs: MockFileSystem{
				MockRemoveAll: func() error { return nil },
				MockTempFile: func() (document.File, error) {
					return TestFile{
						MockWrite: func() (int, error) { return 0, ErrTempFileError },
						MockName:  func() string { return filenameRC },
						MockClose: func() error { return nil },
					}, nil
				}},
		},
	}
	for _, test := range tests {
		kctl.FileSystem = test.fs
		assert.Equal(t, kctl.Apply(docs, ao), test.expectedErr)
	}
}
