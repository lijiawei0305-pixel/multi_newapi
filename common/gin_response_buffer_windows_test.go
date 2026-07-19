//go:build windows

package common

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestBufferedResponseWindowsRuntimeUsesProtectedPrivateDACLs(t *testing.T) {
	t.Run("runtime paths have independently verified ACLs", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		configureBufferedResponseTest(t, t.TempDir(), 4, 32, 32, 1)
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		buffer, err := NewBufferedResponseWriter(context.Writer)
		require.NoError(t, err)
		t.Cleanup(buffer.Discard)

		bufferedResponseGate.Lock()
		spoolDir := bufferedResponseGate.spoolDir
		bufferedResponseGate.Unlock()
		require.NotEmpty(t, spoolDir)
		assertWindowsPrivateSpoolSecurity(t, spoolDir, true)
		assertWindowsPrivateSpoolSecurity(t, filepath.Join(spoolDir, ".active.lock"), false)

		_, err = buffer.WriteString("private-spill")
		require.NoError(t, err)
		require.NotEmpty(t, buffer.tempPath)
		assertWindowsPrivateSpoolSecurity(t, buffer.tempPath, false)
	})

	t.Run("production verifier rejects unsafe ACLs and flags", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "private-file")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
		require.NoError(t, secureBufferedResponsePath(path, false))

		currentUser, system := windowsTestPrivateSIDs(t)
		world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
		require.NoError(t, err)
		setWindowsTestDACL(t, path, false, true, []*windows.SID{currentUser, system, world}, 0)
		require.Error(t, verifyBufferedResponsePathSecurity(path, false))

		setWindowsTestDACL(t, path, false, false, []*windows.SID{currentUser, system}, 0)
		require.Error(t, verifyBufferedResponsePathSecurity(path, false))

		for name, extraFlags := range map[string]uint32{
			"inherited":    windows.INHERITED_ACE,
			"inherit-only": windows.INHERIT_ONLY_ACE,
			"no-propagate": windows.NO_PROPAGATE_INHERIT_ACE,
		} {
			t.Run(name, func(t *testing.T) {
				directory := filepath.Join(t.TempDir(), "private-dir")
				require.NoError(t, os.Mkdir(directory, 0o700))
				setWindowsTestDACL(t, directory, true, true, []*windows.SID{currentUser, system}, extraFlags)
				require.Error(t, verifyBufferedResponsePathSecurity(directory, true))
				setWindowsTestDACL(t, directory, true, true, []*windows.SID{currentUser, system}, 0)
				require.NoError(t, os.RemoveAll(directory))
			})
		}
	})

	t.Run("production verifier rejects reparse points and type mismatches", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "target")
		junction := filepath.Join(base, "junction")
		require.NoError(t, os.Mkdir(target, 0o700))
		output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, target).CombinedOutput()
		require.NoErrorf(t, err, "create junction: %s", output)
		require.Error(t, verifyBufferedResponsePathSecurity(junction, true))

		file := filepath.Join(base, "regular-file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		require.NoError(t, secureBufferedResponsePath(file, false))
		require.Error(t, verifyBufferedResponsePathSecurity(file, true))
		require.Error(t, verifyBufferedResponsePathSecurity(target, false))
	})

	t.Run("unsafe writable base and stale directory are rejected", func(t *testing.T) {
		base := t.TempDir()
		currentUser, system := windowsTestPrivateSIDs(t)
		world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
		require.NoError(t, err)
		setWindowsTestDACL(t, base, true, true, []*windows.SID{currentUser, system, world}, 0)
		require.Error(t, verifyBufferedResponseBaseDirSecurity(base))

		cleanupBase := t.TempDir()
		oldDir := filepath.Join(cleanupBase, bufferedResponseInstancePrefix+"foreign")
		require.NoError(t, os.Mkdir(oldDir, 0o700))
		setWindowsTestDACL(t, oldDir, true, true, []*windows.SID{currentUser, system, world}, 0)
		require.NoError(t, cleanupStaleBufferedResponseSpoolDirs(cleanupBase, 0, timeNowForWindowsSpoolTest()))
		_, err = os.Stat(oldDir)
		require.NoError(t, err, "cleanup must preserve a directory whose private ownership cannot be proven")
	})
}

func assertWindowsPrivateSpoolSecurity(t *testing.T, path string, directory bool) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	require.True(t, ok)
	require.Zero(t, attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT)
	require.Equal(t, directory, info.IsDir())

	currentUser, system := windowsTestPrivateSIDs(t)
	defaultOwner := windowsTestDefaultOwnerSID(t)
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	require.NoError(t, err)
	owner, _, err := descriptor.Owner()
	require.NoError(t, err)
	require.True(t, owner.Equals(defaultOwner), "owner must match the access token's default owner")
	control, _, err := descriptor.Control()
	require.NoError(t, err)
	require.NotZero(t, control&windows.SE_DACL_PROTECTED)
	dacl, defaulted, err := descriptor.DACL()
	require.NoError(t, err)
	require.False(t, defaulted)
	require.NotNil(t, dacl)

	expected := map[string]struct{}{currentUser.String(): {}}
	if !currentUser.Equals(system) {
		expected[system.String()] = struct{}{}
	}
	require.Equal(t, len(expected), int(dacl.AceCount))
	wantFlags := uint8(0)
	if directory {
		wantFlags = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		require.NoError(t, windows.GetAce(dacl, uint32(index), &ace))
		require.Equal(t, uint8(windows.ACCESS_ALLOWED_ACE_TYPE), ace.Header.AceType)
		require.Equal(t, wantFlags, ace.Header.AceFlags)
		const fileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
		require.True(t, ace.Mask&windows.GENERIC_ALL != 0 || ace.Mask&fileAllAccess == fileAllAccess)
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		require.True(t, sid.IsValid())
		_, found := expected[sid.String()]
		require.True(t, found, "unexpected principal %s", sid.String())
		delete(expected, sid.String())
	}
	require.Empty(t, expected)
}

func windowsTestPrivateSIDs(t *testing.T) (*windows.SID, *windows.SID) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	require.NoError(t, err)
	require.NotNil(t, user)
	require.NotNil(t, user.User.Sid)
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	require.NoError(t, err)
	return user.User.Sid, system
}

type windowsTestTokenOwner struct {
	Owner *windows.SID
}

func windowsTestDefaultOwnerSID(t *testing.T) *windows.SID {
	t.Helper()
	token := windows.GetCurrentProcessToken()
	var ownerSize uint32
	err := windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &ownerSize)
	require.ErrorIs(t, err, windows.ERROR_INSUFFICIENT_BUFFER)
	require.GreaterOrEqual(t, ownerSize, uint32(unsafe.Sizeof(windowsTestTokenOwner{})))
	ownerBuffer := make([]byte, ownerSize)
	require.NoError(t, windows.GetTokenInformation(token, windows.TokenOwner, &ownerBuffer[0], ownerSize, &ownerSize))
	owner := (*windowsTestTokenOwner)(unsafe.Pointer(&ownerBuffer[0])).Owner
	require.NotNil(t, owner)
	require.True(t, owner.IsValid())
	ownerCopy, err := owner.Copy()
	require.NoError(t, err)
	return ownerCopy
}

func setWindowsTestDACL(t *testing.T, path string, directory bool, protected bool, principals []*windows.SID, extraFlags uint32) {
	t.Helper()
	inheritance := uint32(windows.NO_INHERITANCE)
	if directory {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	entries := make([]windows.EXPLICIT_ACCESS, 0, len(principals))
	for _, principal := range principals {
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance | extraFlags,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(principal),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	require.NoError(t, err)
	securityInformation := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if protected {
		securityInformation = windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	}
	require.NoError(t, windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, securityInformation, nil, nil, acl, nil))
}

func timeNowForWindowsSpoolTest() time.Time {
	return time.Now().Add(time.Hour)
}
