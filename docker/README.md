# Container users

The alltools and integration-test images run as UID/GID 1000 by default, with
`HOME=/home/bsc`. Override `BSC_USER_UID` and `BSC_USER_GID` when building to match
the ownership of bind-mounted files. Run `make truffle-test` as a non-root host
user; it builds the test images with that user's UID.

Mount existing alltools data at `/home/bsc/.ethereum` instead of `/root/.ethereum`,
or pass an explicit `--datadir`. Clef's default directory is now `/home/bsc/.clef`.
Host data is unchanged; its permissions must allow access by the container user.
Existing root-owned test output may need its ownership corrected before rerunning
the integration tests. Containers do not change host file ownership on startup.

The main node image's `/bsc` home and `/data` volume are unchanged.
