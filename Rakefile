task default: :build

desc 'Build extract into bin/extract'
task :build do
  sh 'scripts/build.sh'
end

desc 'Build, then install bin/extract on HOST as /usr/bin/extract'
task :push, [:host] do |_t, args|
  abort 'usage: rake push[HOST]' if args[:host].to_s.empty?
  Rake::Task[:build].invoke
  sh 'scripts/push', args[:host]
end
