# Extract::Config tests

use Test::More;
use FindBin qw($Bin);

use_ok('Extract::Config');

my ($c);

ok($c = Extract::Config->new("$Bin/t01/extract.conf"), 'construction ok: ' . $c);
is($c->{EXTRACT_ROOT}, '/data/extract', 'EXTRACT_ROOT ok: ' . $c->{EXTRACT_ROOT});
is($c->{EXTRACT_HOST_CMD}, 'hosttag -A', 'EXTRACT_HOST_CMD ok: ' . $c->{EXTRACT_HOST_CMD});

done_testing;

