# Packaging sinty-accounts on Sinty

sinty-accounts owns the same bus name as accounts-daemon, so nothing downstream changes.
Packaging installs the daemon, its bus policy and activation file, and drops accountsservice.

## Files

- Binary: build static, install as `/usr/libexec/sinty-accountsd`.

      CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o sinty-accountsd ./cmd/sinty-accountsd

- D-Bus policy: `org.freedesktop.Accounts.conf` to `/usr/share/dbus-1/system.d/`
  (replaces the one accountsservice shipped; without it the bus refuses the name).
- Activation: `org.freedesktop.Accounts.service` to `/usr/share/dbus-1/system-services/`,
  so the first client starts the daemon even before the unit runs.
- Unit: `sinty-accounts.service` to `/usr/lib/systemd/system/`.

## Enable under sinit

sinit resolves enablement from the target's `.wants` directory and does not pull
`multi-user.target.wants` from `graphical.target`, so link it into the graphical one:

    ln -sf /usr/lib/systemd/system/sinty-accounts.service \
       /usr/lib/systemd/system/graphical.target.wants/sinty-accounts.service

The unit is deliberately minimal (`Type=dbus`, no systemd sandboxing directives, which
break under sinit).

## Notes

- Do not ship accountsservice alongside: two daemons cannot own the name. Removing it
  also removes the last consumer of `libsystemd-shim` in the desktop stack.
- `/var/lib/AccountsService` must be writable. Existing state files and icons from
  accountsservice are read as they are.
- The shadow tools (`useradd`, `usermod`, `userdel`, `gpasswd`, `chage`) must be in
  the image; `BR2_PACKAGE_SHADOW` provides them.
