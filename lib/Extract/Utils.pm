package Extract::Utils;

use strict;
use Exporter::Lite;
use Carp;

our @EXPORT = qw();
our @EXPORT_OK = qw(run_scripts);

sub run_scripts
{
  my %args = @_;
  my $type = delete $args{type}
    or croak "Missing required argument 'type'";
  my $scripts = delete $args{scripts}
    or croak "Missing required argument 'scripts'";
  my $params = delete $args{params}
    or croak "Missing required argument 'params'";
  my $config = delete $args{config}
    or croak "Missing required argument 'config'";

  for my $script (@$scripts) {
    $script = File::Spec->rel2abs( $script, $config->{SCRIPTS_DIR} );
    unless (-x $script) {
      print "Non-executable $type script $script - skipping\n";
      next;
    }
    print "+ $type $script ...\n" if $args{verbose} or $args{noop};
    my $params_list = join ' ', @$params;
    my $out = qx($script $params_list) unless $args{noop};
    print $out if $out && $args{verbose};
  }
}

1;

