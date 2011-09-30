package Extract::Config;

use strict;
use warnings;
use Config::Tiny;
use File::Spec;
use FindBin qw($RealBin);
use Carp;

sub _init {
  my $conf_file = shift;

  # Setup default directories
  my $config_dir = File::Spec->catdir( File::Spec->rootdir, 'etc', 'extract' );
  $config_dir = File::Spec->catdir( $RealBin, File::Spec->updir, 'etc' )->canonpath
    unless -d $config_dir;
  my $scripts_dir = File::Spec->catdir( $config_dir, 'scripts' );

  # Read config file
  my $config = Config::Tiny->read($conf_file);
  $config ||= Config::Tiny->read(File::Spec->catfile( $config_dir, 'extract.conf' ));
  croak "Cannot find extract.conf config file '$conf_file'\n" unless $config;

  $config = $config->{_};
  s/(^["']|["']$)//g foreach values %$config;   # dequote values
  $config->{SCRIPTS_DIR} = $scripts_dir;

  return $config;
}

sub new {
  my $class = shift;
  my $self = _init(@_);
  bless $self, $class;
}

1;

