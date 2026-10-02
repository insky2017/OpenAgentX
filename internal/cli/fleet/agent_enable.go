package fleet

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
	fleetmodel "openagentx/internal/fleet"
)

// Only an explicitly resumed prepared Agent is promoted into bulk Fleet startup.
// Call after Worker, generation, network and readiness checks have all succeeded.
func enablePreparedAgent(o agentOptions, deps Dependencies) error {
	receipt, err := fleetmodel.ReadSecureFile(joinReceiptPath(o), fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var prepared joinReceipt
	if err = json.Unmarshal(receipt, &prepared); err != nil {
		return err
	}
	if prepared.Version != 1 || prepared.AgentID != o.id {
		return fmt.Errorf("invalid prepared Agent receipt")
	}
	fd, err := unix.Open(o.paths.manifest+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err = unix.Flock(fd, unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	original, err := fleetmodel.ReadSecureFile(o.paths.manifest, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
	if err != nil {
		return err
	}
	manifest, err := fleetmodel.Decode(bytes.NewReader(original))
	if err != nil {
		return err
	}
	found := false
	for i := range manifest.Agents {
		if manifest.Agents[i].AgentID == o.id {
			found = true
			if manifest.Agents[i].Enabled {
				return nil
			}
			manifest.Agents[i].Enabled = true
		}
	}
	if !found {
		return fmt.Errorf("prepared Agent is no longer in Fleet")
	}
	content, err := fleetmodel.Encode(manifest)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(o.paths.manifest), ".fleet-enable-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err = temp.Write(content); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if deps.BeforeAtomicRename != nil {
		if err = deps.BeforeAtomicRename(o.paths.manifest); err != nil {
			return err
		}
	}
	current, err := fleetmodel.ReadSecureFile(o.paths.manifest, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return fmt.Errorf("Fleet manifest changed during Agent activation")
	}
	if err = os.Rename(temp.Name(), o.paths.manifest); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(o.paths.manifest))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
