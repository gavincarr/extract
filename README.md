
Extract
=======

extract is a utility for copying a set of files off one or more remote
hosts into a set of local host trees. It's implemented using rsync.

extract requires the list of files to extract to be available either
locally or on the remote machine. I typically use
[rpm-find-changes](http://ofn.me/rfc) to generate a list of files to
extract on remote RedHat/CentOS machines.

Setup
-----

RPMs for RedHat/CentOS 5/6 are available from http://ofn.me/ofrepo.

Or you can run from a git checkout, as follows:

```shell
git clone https://github.com/gavincarr/extract/
cd extract
cp conf/extract.conf.dist conf/extract.conf
$EDITOR conf/extract.conf
extract -c conf/extract.conf -v
```

See the config file (and `perldoc bin/extract`) for documentation.


Author and Licence
------------------

Copyright 2007-2012 Gavin Carr <gavin@openfusion.com.au>.

extract is licensed under the terms of the GNU General Public Licence
Version 3. See the COPYING file for details.

