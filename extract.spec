
Name: extract
Summary: extract is a tool for periodically copying data from a remote machine
Version: 0.1
Release: 1%{?org_tag}
Source0: %{name}-%{version}.tar.gz
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

install bin/extract %{buildroot}%{_bindir}
cp -p etc/extract.conf.dist %{buildroot}%{_sysconfdir}/extract/extract.conf
cp -p etc/scripts/* %{buildroot}%{_sysconfdir}/extract/scripts

%clean
[ "%{buildroot}" != "/" ] && rm -rf %{buildroot}

%files
%defattr(-,root,root)
%{_bindir}/*
%config(noreplace) %{_sysconfdir}/extract/*


%changelog
* Wed Jun 27 2007 Gavin Carr <gavin@openfusion.com.au> 0.1-1
- Initial spec file.

