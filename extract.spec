
Name: extract
Summary: extract is a tool for periodically copying data from a remote machine
Version: 0.1
Release: 1%{?org_tag}%{?dist}
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
mkdir -p %{buildroot}%{_sysconfdir}/extract

install bin/extract %{buildroot}%{_bindir}
cp etc/* %{buildroot}%{_sysconfdir}/extract

%clean
[ "%{buildroot}" != "/" ] && rm -rf %{buildroot}

%files
%defattr(-,root,root)
%{_bindir}/*
%config(noreplace) %{_sysconfdir}/extract/*

