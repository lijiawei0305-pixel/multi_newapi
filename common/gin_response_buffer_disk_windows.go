//go:build windows

package common

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func createPrivateBufferedResponseTempDir(baseDir string, prefix string) (string, error) {
	descriptor, err := privateBufferedResponseSecurityDescriptor(true)
	if err != nil {
		return "", err
	}
	attributes := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	for range 100 {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return "", fmt.Errorf("generate private spool directory name: %w", err)
		}
		path := filepath.Join(baseDir, prefix+hex.EncodeToString(random))
		pathPointer, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return "", err
		}
		err = windows.CreateDirectory(pathPointer, attributes)
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := verifyBufferedResponsePathSecurity(path, true); err != nil {
			_ = os.RemoveAll(path)
			return "", err
		}
		return path, nil
	}
	return "", errors.New("could not allocate a unique private spool directory")
}

func createBufferedResponseOwnershipLock(path string) (*os.File, error) {
	return openBufferedResponseOwnershipLockWithDisposition(path, windows.CREATE_NEW)
}

func openBufferedResponseOwnershipLock(path string) (*os.File, error) {
	return openBufferedResponseOwnershipLockWithDisposition(path, windows.OPEN_EXISTING)
}

func openBufferedResponseOwnershipLockWithDisposition(path string, disposition uint32) (*os.File, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		disposition,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func removeLockedBufferedResponseSpoolDir(path string, lockFile *os.File) error {
	if lockFile == nil {
		return errors.New("stale response spool ownership lock is nil")
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		_ = lockFile.Close()
		return fmt.Errorf("generate stale spool quarantine name: %w", err)
	}
	quarantine := filepath.Join(filepath.Dir(path), filepath.Base(path)+".deleting-"+hex.EncodeToString(random))
	// Windows refuses to rename a directory while any file below it has an
	// open handle, even when that file was opened with FILE_SHARE_DELETE. The
	// exclusive byte-range lock already proved that this random instance
	// directory has no live owner, and instance directories are never reused,
	// so release the lock handle immediately before the quarantine rename.
	if err := lockFile.Close(); err != nil {
		return fmt.Errorf("close response spool ownership lock before quarantine: %w", err)
	}
	if err := os.Rename(path, quarantine); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("quarantine stale response spool: %w", err)
	}
	return os.RemoveAll(quarantine)
}

type bufferedResponseTokenOwner struct {
	Owner *windows.SID
}

func privateBufferedResponseSIDs() (*windows.SID, *windows.SID, *windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	currentUser, err := token.GetTokenUser()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get current process user: %w", err)
	}
	if currentUser == nil || currentUser.User.Sid == nil || !currentUser.User.Sid.IsValid() {
		return nil, nil, nil, errors.New("current process user SID is invalid")
	}
	var ownerSize uint32
	err = windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &ownerSize)
	if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return nil, nil, nil, fmt.Errorf("get current process default owner size: %w", err)
	}
	if ownerSize < uint32(unsafe.Sizeof(bufferedResponseTokenOwner{})) {
		return nil, nil, nil, errors.New("current process default owner information is truncated")
	}
	ownerBuffer := make([]byte, ownerSize)
	if err := windows.GetTokenInformation(token, windows.TokenOwner, &ownerBuffer[0], ownerSize, &ownerSize); err != nil {
		return nil, nil, nil, fmt.Errorf("get current process default owner: %w", err)
	}
	defaultOwner := (*bufferedResponseTokenOwner)(unsafe.Pointer(&ownerBuffer[0])).Owner
	if defaultOwner == nil || !defaultOwner.IsValid() {
		return nil, nil, nil, errors.New("current process default owner SID is invalid")
	}
	defaultOwner, err = defaultOwner.Copy()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("copy current process default owner SID: %w", err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create LocalSystem SID: %w", err)
	}
	return currentUser.User.Sid, system, defaultOwner, nil
}

func privateBufferedResponseSecurityDescriptor(directory bool) (*windows.SECURITY_DESCRIPTOR, error) {
	currentUser, system, _, err := privateBufferedResponseSIDs()
	if err != nil {
		return nil, err
	}
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	sddl := fmt.Sprintf("D:P(A;%s;GA;;;%s)", inheritance, currentUser.String())
	if !currentUser.Equals(system) {
		sddl += fmt.Sprintf("(A;%s;GA;;;%s)", inheritance, system.String())
	}
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, fmt.Errorf("build private spool security descriptor: %w", err)
	}
	return descriptor, nil
}

func secureBufferedResponsePath(path string, directory bool) error {
	currentUser, system, _, err := privateBufferedResponseSIDs()
	if err != nil {
		return err
	}
	inheritance := uint32(windows.NO_INHERITANCE)
	if directory {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	principals := []*windows.SID{currentUser}
	if !currentUser.Equals(system) {
		principals = append(principals, system)
	}
	entries := make([]windows.EXPLICIT_ACCESS, 0, len(principals))
	for _, principal := range principals {
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(principal),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return fmt.Errorf("build private spool DACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return fmt.Errorf("set private spool DACL: %w", err)
	}
	return verifyBufferedResponsePathSecurity(path, directory)
}

func verifyBufferedResponseBaseDirSecurity(path string) error {
	if err := verifyBufferedResponseWindowsPathType(path, true); err != nil {
		return err
	}
	currentUser, system, defaultOwner, err := privateBufferedResponseSIDs()
	if err != nil {
		return err
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("create Administrators SID: %w", err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("read response spool base security descriptor: %w", err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return fmt.Errorf("read response spool base owner: %w", err)
	}
	if owner == nil || !owner.IsValid() || (!owner.Equals(currentUser) && !owner.Equals(defaultOwner)) {
		return errors.New("response spool base directory owner is not trusted by the current access token")
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read response spool base DACL: %w", err)
	}
	if dacl == nil || defaulted {
		return errors.New("response spool base directory has an unsafe default DACL")
	}
	trusted := map[string]struct{}{
		currentUser.String():    {},
		system.String():         {},
		administrators.String(): {},
	}
	const unsafeBasePermissions = windows.GENERIC_ALL | windows.GENERIC_WRITE | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | 0x2 | 0x4 | 0x40
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return fmt.Errorf("read response spool base access entry %d: %w", index, err)
		}
		if ace == nil || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("response spool base has unsupported effective access entry %d", index)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid == nil || !sid.IsValid() {
			return fmt.Errorf("response spool base access entry %d has an invalid SID", index)
		}
		if _, ok := trusted[sid.String()]; ok {
			continue
		}
		if ace.Mask&unsafeBasePermissions != 0 {
			return fmt.Errorf("response spool base grants mutation access to unexpected principal %s", sid.String())
		}
	}
	return nil
}

func verifyBufferedResponsePathSecurity(path string, directory bool) error {
	if err := verifyBufferedResponseWindowsPathType(path, directory); err != nil {
		return err
	}
	currentUser, system, defaultOwner, err := privateBufferedResponseSIDs()
	if err != nil {
		return err
	}
	expected := map[string]struct{}{currentUser.String(): {}}
	if !currentUser.Equals(system) {
		expected[system.String()] = struct{}{}
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("read private spool DACL: %w", err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return fmt.Errorf("read private spool owner: %w", err)
	}
	if owner == nil || !owner.IsValid() || !owner.Equals(defaultOwner) {
		return errors.New("private spool path owner does not match the current access token's default owner")
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return fmt.Errorf("read private spool DACL control: %w", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("private spool DACL inherits access from its parent")
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read private spool access entries: %w", err)
	}
	if defaulted {
		return errors.New("private spool DACL is defaulted")
	}
	if dacl == nil || int(dacl.AceCount) != len(expected) {
		return fmt.Errorf("private spool DACL has %d entries, want %d", daclEntryCount(dacl), len(expected))
	}
	wantInheritance := uint8(0)
	if directory {
		wantInheritance = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return fmt.Errorf("read private spool access entry %d: %w", index, err)
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("private spool access entry %d is not an allow entry", index)
		}
		const fileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
		if ace.Mask&windows.GENERIC_ALL == 0 && ace.Mask&fileAllAccess != fileAllAccess {
			return fmt.Errorf("private spool access entry %d does not grant full access", index)
		}
		if ace.Header.AceFlags != wantInheritance {
			return fmt.Errorf("private spool access entry %d flags are %#x, want %#x", index, ace.Header.AceFlags, wantInheritance)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid == nil || !sid.IsValid() {
			return fmt.Errorf("private spool access entry %d has an invalid SID", index)
		}
		if _, ok := expected[sid.String()]; !ok {
			return fmt.Errorf("private spool DACL grants unexpected principal %s", sid.String())
		}
		delete(expected, sid.String())
	}
	if len(expected) != 0 {
		return errors.New("private spool DACL is missing an allowed principal")
	}
	return nil
}

func verifyBufferedResponseWindowsPathType(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attributes == nil {
		return errors.New("private spool Windows attributes are unavailable")
	}
	if attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return errors.New("private spool path is a reparse point")
	}
	if directory && !info.IsDir() {
		return errors.New("private spool directory path is not a directory")
	}
	if !directory && !info.Mode().IsRegular() {
		return errors.New("private spool file path is not a regular file")
	}
	return nil
}

func daclEntryCount(acl *windows.ACL) int {
	if acl == nil {
		return 0
	}
	return int(acl.AceCount)
}

func tryLockBufferedResponseFile(file *os.File) (bool, error) {
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
	if err == nil {
		return true, nil
	}
	if err == windows.ERROR_LOCK_VIOLATION {
		return false, nil
	}
	return false, err
}

func bufferedResponseAvailableDiskBytes(path string) (int64, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &available, nil, nil); err != nil {
		return 0, err
	}
	const maxInt64 = uint64(^uint64(0) >> 1)
	if available > maxInt64 {
		return int64(maxInt64), nil
	}
	return int64(available), nil
}
