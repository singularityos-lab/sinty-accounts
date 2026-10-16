# sinty-accounts

A native replacement for `accounts-daemon` (accountsservice) on SintyOS. It owns
`org.freedesktop.Accounts` on the system bus and serves the same object tree, so
the Singularity settings, the greeter, the lock screen, the portals and any
other client of the Accounts API keep working against it unchanged.

## Why it exists

SintyOS does not boot with systemd. accountsservice is the last desktop service
that still pulls `libsystemd` into the image: it links it to follow logins and
seats, so the image had to carry `libsystemd-shim` only to satisfy it.
Everything the desktop actually asks of it is a thin layer over the user
database, which a small static daemon can serve directly.

`sinty-accounts`:

- reads `/etc/passwd`, `/etc/shadow` and `/etc/group` itself, and notices when
  something else edits them;
- changes accounts through the shadow tools (`useradd`, `usermod`, `userdel`,
  `gpasswd`, `chage`), so the files stay consistent with every other tool;
- keeps per-user settings in `/var/lib/AccountsService/users/<name>` and icons
  in `/var/lib/AccountsService/icons/<name>`, the same places accountsservice
  uses, so an existing system keeps its data;
- authorizes through polkit with the same action ids as accountsservice when
  polkit is running, and falls back to membership of `wheel` (or `sudo`,
  `admin`) when it is not;
- serves the parental controls interfaces (`com.endlessm.ParentalControls.*`)
  out of the box, plus any extension interface described in
  `/usr/share/accountsservice/interfaces/`.

An icon is copied only when it is a regular file, not a symbolic link, an image,
at most 1 MB, and owned by the caller (administrators may use any file). The
daemon runs as root, so this keeps it from copying files the caller could not
read.

## What it serves

`/org/freedesktop/Accounts` implements `org.freedesktop.Accounts`:
`ListCachedUsers`, `FindUserById`, `FindUserByName`, `CreateUser`, `DeleteUser`,
`CacheUser`, `UncacheUser`, `GetUsersLanguages`, the `UserAdded` and
`UserDeleted` signals, and the `DaemonVersion`, `HasNoUsers`,
`HasMultipleUsers` and `AutomaticLoginUsers` properties.

Each person is `/org/freedesktop/Accounts/User<uid>` with
`org.freedesktop.Accounts.User`: every property and setter accountsservice
offers, `GetPasswordExpirationPolicy` and the `Changed` signal. `SetPassword`
takes a crypt(3) hash, as with accountsservice.

Login history (`LoginFrequency`, `LoginTime`, `LoginHistory`) is reported as
empty: SintyOS keeps no wtmp.

## Usage

    sinty-accountsd                       serve on the system bus
    sinty-accountsd -root DIR             use DIR/etc and DIR/var/lib instead of /
    sinty-accountsd -session-bus          serve on the session bus, for testing

## Build

    make build            # ./sinty-accountsd
    make static           # stripped static binary for the image
    make test
    go build ./...        # plain build

Requires Go >= 1.25 and Linux. The only non-stdlib dependency is
`github.com/godbus/dbus/v5`. The tests start a private `dbus-daemon`.

Packaging notes are in `packaging/`.
