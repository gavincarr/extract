
Name: extract
Summary: extract is a tool for periodically copying data from a remote machine
Version: 0.5.2
Release: 1%{?org_tag}%{dist}
Source: %{name}-%{version}.tar.gz
License: GPL
URL: http://www.openfusion.com.au/labs/
Group: Applications/File
BuildRoot: %{_tmppath}/%{name}-%{version}
BuildArch: noarch
Requires: rsync

%description
extract is a tool for periodically copying data (files and command output) from 
a remote machine. The data so extracted can be post-processed by arbitrary user
programs to e.g. archive it, place it under version control, do comparisons and
checks on it, etc.

%prep
%setup

%build
[ "%{buildroot}" != "/" ] && rm -rf %{buildroot}

%install
mkdir -p %{buildroot}%{_bindir}
mkdir -p %{buildroot}%{_sysconfdir}/extract/scripts
mkdir -p %{buildroot}%{_sysconfdir}/cron.d
mkdir -p %{buildroot}%{perl_vendorlib}/Extract

install bin/extract %{buildroot}%{_bindir}
install bin/extract_git_report %{buildroot}%{_bindir}
cp -p lib/Extract/Config.pm %{buildroot}%{perl_vendorlib}/Extract
cp -p conf/extract.conf.dist %{buildroot}%{_sysconfdir}/extract/extract.conf
cp -p conf/cron.d/* %{buildroot}%{_sysconfdir}/cron.d
cp -p scripts/* %{buildroot}%{_sysconfdir}/extract/scripts

%clean
[ "%{buildroot}" != "/" ] && rm -rf %{buildroot}

%files
%defattr(-,root,root)
%{_bindir}/*
%{perl_vendorlib}/Extract/*
%config(noreplace) %{_sysconfdir}/extract/*.conf
%config(noreplace) %{_sysconfdir}/extract/scripts/*
%config(noreplace) %{_sysconfdir}/cron.d/*


%changelog
* Thu Oct 06 2011 Gavin Carr <gavin@openfusion.com.au> 0.5.2-1
- Fix bug with remote files-from handling.

* Wed Oct 05 2011 Gavin Carr <gavin@openfusion.com.au> 0.5.1-1
- Tweak extract to still run post-extract hooks after errors.

* Wed Oct 05 2011 Gavin Carr <gavin@openfusion.com.au> 0.5-1
- Add EXTRACT_TREE and EXTRACT_FILES_LOCAL options.

* Fri Sep 30 2011 Gavin Carr <gavin@openfusion.com.au> 0.4-1
- Split out config loading into separate Extract::Config module.
- Tweak extract.conf to be usable from shell.
- Move EXTRACT_GIT_REPO_MODE into extract.conf.
- Add extract_git_report shell script.

* Wed Nov 04 2009 Gavin Carr <gavin@openfusion.com.au> 0.3.1-1
- Add post_extract_touch script.
- Allow non-absolute script filenames in extract.conf (in /etc/extract/scripts).

* Tue Nov 03 2009 Gavin Carr <gavin@openfusion.com.au> 0.3-1
- Add --delete option to extract rsync.
- Add host_cmd support to extract.
- Add a post_extract_git script.
- Script cleanups.

* Thu Dec 20 2007 Gavin Carr <gavin@openfusion.com.au> 0.2.6-1
- Add a bzr revert to post_extract_bzr script.

* Thu Aug 30 2007 Gavin Carr <gavin@openfusion.com.au> 0.2.5-1
- Tweak extract script parameters again - now pass root, host, file.

* Thu Aug 30 2007 Gavin Carr <gavin@openfusion.com.au> 0.2.4-1
- Add filelist file to parameters passed to post_extract scripts.

* Tue Aug 28 2007 Gavin Carr <gavin@openfusion.com.au> 0.2.3-1
- Add a -c <config_file> argument.

* Fri Jul 27 2007 Gavin Carr <gavin@openfusion.com.au> 0.2.2-1
- More error-handling tweaks to pre_extract_bzr.

* Fri Jul 27 2007 Gavin Carr <gavin@openfusion.com.au> 0.2.1-1
- Tweaks to pre_extract_bzr.

* Thu Jul 26 2007 Gavin Carr <gavin@openfusion.com.au> 0.2-1
- Add support for pre_extract_host scripts.
- Add example pre_extract_delete script.
- Add example post_extract_bzr script.

* Thu Jun 28 2007 Gavin Carr <gavin@openfusion.com.au> 0.1.5-1
- Tweak error handling in extract itself.

* Thu Jun 28 2007 Gavin Carr <gavin@openfusion.com.au> 0.1.4-1
- Fix more find version problems in extract_tarball.

* Thu Jun 28 2007 Gavin Carr <gavin@openfusion.com.au> 0.1.3-1
- Fix find version problems in extract_tarball.

* Thu Jun 28 2007 Gavin Carr <gavin@openfusion.com.au> 0.1.2-1
- Add a sample cron job to spec file.

* Thu Jun 28 2007 Gavin Carr <gavin@openfusion.com.au> 0.1.1-1
- Add support for user@host format hosts.
- Various minor bug fixes.

* Wed Jun 27 2007 Gavin Carr <gavin@openfusion.com.au> 0.1-1
- Initial spec file.

