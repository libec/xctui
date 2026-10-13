#!/usr/bin/env ruby
# Black-box conformance check for any xctui build.
#
#   ruby check.rb <binary> [cli|tui|all] [reference-binary]
#
# cli: runs the `cli` cases of ../tests.yaml against the binary with fake xcodebuild, xcrun and
#      open on PATH, in a temp folder, and compares stdout, stderr and the exit code.
# tui: opens the binary in tmux (100x30 unless noted) in a fake project with the fixture's
#      schemes and destinations, presses keys, captures the screen; with a reference binary the
#      captures are diffed against the reference's (times masked), else they are printed.
# Needs ruby and tmux. Nothing outside the temp folders is touched: HOME points into them.
require 'yaml'
require 'json'
require 'tmpdir'
require 'fileutils'
require 'shellwords'

SPEC = File.expand_path('..', __dir__)
CASES = YAML.load_file(File.join(SPEC, 'tests.yaml'), aliases: true)
FIX = File.join(SPEC, 'fixtures')
BIN = File.expand_path(ARGV[0] || abort("usage: check.rb <binary> [cli|tui|all] [reference]"))
MODE = ARGV[1] || 'all'
REF = ARGV[2] && File.expand_path(ARGV[2])
DEADLINE = 20 # seconds a cli case may take before it is killed and counted as failed

def tmpdir = Dir.mktmpdir('xctui-check-', File.realpath(Dir.tmpdir))

def make_tree(dir, tree)
  entries = tree.is_a?(Hash) ? tree.to_a : Array(tree).map { |e| e.is_a?(Hash) ? e.to_a.first : [e, nil] }
  entries.each do |path, body|
    full = File.join(dir, path)
    if body.nil? && path.end_with?('/') then FileUtils.mkdir_p(full)
    else FileUtils.mkdir_p(File.dirname(full)); File.write(full, body || '') end
  end
end

def script(path, body)
  File.write(path, "#!/bin/sh\n#{body}\n"); File.chmod(0o755, path)
end

def printf_lines(lines) = lines.map { |l| "printf '%s\\n' #{Shellwords.escape(l)}" }.join("\n")

# Fake tools. build_lines/build_exit shape the xcodebuild action; stopped makes it wait for
# SIGTERM after the first line. launch_lines are what `simctl launch` prints; it then exits by
# itself after launch_exit_after seconds, or waits for SIGINT when that is nil. log_lines are
# what `log stream` prints, half a second after it starts so that the app's first stdout line
# always lands in the console before the log's (the two arrive from two processes; the
# reference itself shows either order), before waiting for SIGTERM.
def fake_tools(bin, simctl_json:, devicectl_json: nil, build_lines: [], build_exit: 0, stopped: false, list_json: nil,
               launch_lines: ['hello from stdout'], launch_exit_after: nil,
               log_lines: ['Filtering the log data using "process == \"Puzzle\""',
                           '2026-09-26 10:00:03.361 Df Puzzle[19921:134cebb] [com.apple.UIKit:Application] os_log line'])
  FileUtils.mkdir_p(bin)
  action = if stopped
    "#{printf_lines(build_lines[0, 1])}\nsecond() { #{printf_lines(build_lines[1, 1])}; }\ntrap 'second; exit 143' TERM\nwhile :; do sleep 0.1; done"
  else
    "#{printf_lines(build_lines)}\nsleep 1.5\nexit #{build_exit}"
  end
  list = list_json ? "printf '%s' #{Shellwords.escape(list_json)}" : "cat #{Shellwords.escape(File.join(FIX, 'xcodebuild-list.json'))}"
  script(File.join(bin, 'xcodebuild'), <<~SH)
    case "$*" in
      *-list*) #{list} ;;
      *-showBuildSettings*) cat #{Shellwords.escape(File.join(FIX, 'xcodebuild-showbuildsettings.json'))} ;;
      *) #{action} ;;
    esac
  SH
  launch_end = launch_exit_after ? "sleep #{launch_exit_after}; exit 0" : "trap 'exit 130' INT; while :; do sleep 0.1; done"
  script(File.join(bin, 'xcrun'), <<~SH)
    case "$*" in
      *"simctl list"*) cat #{Shellwords.escape(simctl_json)} ;;
      *"devicectl list"*) #{devicectl_json ? "cat #{Shellwords.escape(devicectl_json)}" : 'exit 0'} ;;
      *"simctl launch"*) #{printf_lines(launch_lines)}; #{launch_end} ;;
      *"log stream"*) sleep 0.5; #{printf_lines(log_lines)}; trap 'exit 143' TERM; while :; do sleep 0.1; done ;;
      *) exit 0 ;;
    esac
  SH
  script(File.join(bin, 'open'), 'exit 0')
end

# Runs the binary; stop_after_first_line sends SIGINT that many seconds after stdout's first
# line. Returns [stdout, stderr, exit code, timed_out, leftovers]. Whatever is still alive at the
# deadline (the binary, or fake tools it failed to stop) is killed; leftovers counts the fakes
# that were still running when the binary had exited.
def run(bin_dir, bin, dir, args, env, stop_after_first_line: nil)
  out_r, out_w = IO.pipe; err_r, err_w = IO.pipe
  pid = Process.spawn(env, bin, *args, chdir: dir, out: out_w, err: err_w)
  out_w.close; err_w.close
  out = +''; err = +''
  readers = [Thread.new { out_r.each_line { |l| out << l } }, Thread.new { err_r.each_line { |l| err << l } }]
  started = Time.now; sent = false; timed_out = false; status = nil
  loop do
    if stop_after_first_line && !sent && out.include?("\n")
      sleep stop_after_first_line; (Process.kill('INT', pid) rescue nil); sent = true
    end
    _, status = Process.wait2(pid, Process::WNOHANG)
    break if status
    if Time.now - started > DEADLINE
      timed_out = true; (Process.kill('KILL', pid) rescue nil); _, status = Process.wait2(pid); break
    end
    sleep 0.05
  end
  sleep 0.3
  leftovers = `pgrep -f #{Shellwords.escape(bin_dir)}`.lines.size
  system('pkill', '-9', '-f', bin_dir, err: File::NULL) if leftovers > 0
  readers.each { |t| t.join(2) }
  [out, err, status.exitstatus || (128 + status.termsig.to_i), timed_out, leftovers]
end

def show(label, want, got)
  return if want == got
  puts "    #{label} differs"
  w, g = want.lines, got.lines
  (0...[w.size, g.size].max).each do |i|
    next if w[i] == g[i]
    puts "      want: #{w[i].inspect}\n      got:  #{g[i].inspect}"; break
  end
end

def match_lines(text, patterns)
  lines = text.lines.map(&:chomp)
  lines.size == patterns.size && lines.zip(patterns).all? { |l, p| Regexp.new(p).match?(l) }
end

def check_cli
  cli = CASES['cli']; pass = 0; total = 0
  cli.each do |name, c|
    next if name == 'tree'
    total += 1
    dir = tmpdir; home = File.join(dir, 'home'); bin = File.join(dir, 'bin'); work = File.join(dir, 'w')
    FileUtils.mkdir_p([home, work])
    make_tree(work, c.key?('tree') ? c['tree'] : cli['tree'])
    lines = (c['tool_output'] || []).map { |l| l.gsub('<tmp>', work) }
    opts = { simctl_json: File.join(FIX, 'simctl-list.json'), devicectl_json: File.join(FIX, 'devicectl-list.json'), build_lines: lines,
             build_exit: c['tool_exit'] || 0, stopped: name == 'build_stopped' }
    opts[:launch_lines] = c['launch_output'] if c['launch_output']
    opts[:launch_exit_after] = c['launch_exit_after_s'] if c['launch_exit_after_s']
    opts[:log_lines] = c['log_output'] if c['log_output']
    fake_tools(bin, **opts)
    env = { 'HOME' => home, 'PATH' => "#{bin}:#{ENV['PATH']}" }
    out, err, code, timed_out, leftovers = run(bin, BIN, work, c['args'], env, stop_after_first_line: (name == 'build_stopped' ? 1.5 : nil))
    ok = !timed_out
    ok &&= (out == c['stdout'].gsub('<tmp>', work)) if c.key?('stdout')
    ok &&= (err == c['stderr'].gsub('<tmp>', work)) if c.key?('stderr')
    ok &&= err.start_with?(c['stderr_starts_with']) if c.key?('stderr_starts_with')
    ok &&= match_lines(out, c['stdout_matches']) if c.key?('stdout_matches')
    ok &&= match_lines(err, c['stderr_matches']) if c.key?('stderr_matches')
    ok &&= (code == c['exit'])
    ok &&= (leftovers == 0) if c['no_process_left']
    puts "  #{ok ? 'PASS' : 'FAIL'} cli.#{name}#{timed_out ? ' (timed out)' : ''}"
    unless ok
      show('stdout', c['stdout'].to_s.gsub('<tmp>', work), out) if c.key?('stdout')
      show('stderr', c['stderr'].to_s.gsub('<tmp>', work), err) if c.key?('stderr')
      puts "    stdout does not match the patterns:\n#{out.lines.map { |l| '      ' + l }.join}" if c.key?('stdout_matches') && !match_lines(out, c['stdout_matches'])
      puts "    stderr does not match the patterns:\n#{err.lines.map { |l| '      ' + l }.join}" if c.key?('stderr_matches') && !match_lines(err, c['stderr_matches'])
      puts "    exit: want #{c['exit']}, got #{code}" if code != c['exit']
      puts "    #{leftovers} fake tool process(es) left running" if c['no_process_left'] && leftovers > 0
    end
    pass += 1 if ok
  ensure
    FileUtils.rm_rf(dir) if dir
  end
  puts "cli: #{pass}/#{total}"
  pass == total
end

# The TUI fixture from tests.yaml, as a fake project and fake simctl output. Schemes come through
# the -list fallback so their order is the fixture's; Net is a local package product.
def tui_env(dir, build_exit:)
  home = File.join(dir, 'home'); bin = File.join(dir, 'bin'); work = File.join(dir, 'src')
  FileUtils.mkdir_p([home, File.join(work, 'Puzzle.xcodeproj'), File.join(work, 'Net')])
  File.write(File.join(work, 'Net', 'Package.swift'), 'let package = Package(name: "Net", products: [.library(name: "Net", targets: [])])')
  f = CASES['tui']['fixture']
  devices = Hash.new { |h, k| h[k] = [] }
  f['destinations'].each do |d|
    platform, version = d['runtime'].split(' ', 2)
    devices["com.apple.CoreSimulator.SimRuntime.#{platform}-#{version.tr('.', '-')}"] << { 'name' => d['name'], 'state' => d['state'], 'udid' => d['udid'] }
  end
  simctl = File.join(dir, 'simctl.json'); File.write(simctl, JSON.generate({ 'devices' => devices }))
  list = JSON.generate({ 'project' => { 'schemes' => f['schemes'].reject { |s| s['package'] }.map { |s| s['name'] } } })
  failing = ['CompileSwift normal arm64 ' + work + '/Puzzle/Game.swift',
             work + "/Puzzle/Game.swift:3:1: error: cannot find 'x' in scope",
             work + "/Puzzle/Game.swift:3:1: error: cannot find 'x' in scope",
             work + "/Puzzle/Board.swift:9:5: warning: unused variable 'y'",
             '** BUILD FAILED **']
  passing = ['CompileSwift normal arm64 ' + work + '/Puzzle/Game.swift', '** BUILD SUCCEEDED **']
  fake_tools(bin, simctl_json: simctl, list_json: list, build_lines: build_exit == 0 ? passing : failing, build_exit: build_exit)
  [bin, work, { 'HOME' => home, 'PATH' => "#{bin}:#{ENV['PATH']}", 'TERM' => 'xterm-256color' }]
end

def normalize(screen)
  screen.lines.map { |l| l.chomp.gsub(/\t/) { ' ' * 8 }.rstrip.gsub(/ +/, ' ') }
        .map { |l| l.gsub(/\b\d\d:\d\d(:\d\d\.\d+)?\b/, 'HH:MM').gsub(/\b\d+s\b/, 'Ns') }.join("\n")
end

def capture(bin_dir, bin, work, env, width, height, keys)
  sess = "xcheck#{$$}"
  system('tmux', 'kill-session', '-t', sess, err: File::NULL)
  cmd = env.map { |k, v| "#{k}=#{Shellwords.escape(v)}" }.join(' ') + ' ' + Shellwords.escape(bin)
  system('tmux', 'new-session', '-d', '-s', sess, '-x', width.to_s, '-y', height.to_s, '-c', work, "env #{cmd}")
  sleep 1.5
  keys.each { |k| k.is_a?(Numeric) ? sleep(k) : (system('tmux', 'send-keys', '-t', sess, k); sleep 0.3) }
  screen = `tmux capture-pane -t #{sess} -p`
  %w[Escape Escape q].each { |k| system('tmux', 'send-keys', '-t', sess, k); sleep 0.2 }
  sleep 0.7; system('tmux', 'kill-session', '-t', sess, err: File::NULL)
  system('pkill', '-9', '-f', bin_dir, err: File::NULL)
  normalize(screen)
end

# name, width, height, keys, exit code of the fake xcodebuild
SEQUENCES = [
  ['startup-80x24', 80, 24, [], 65],
  ['startup-100x30', 100, 30, [], 65],
  ['startup-140x30', 140, 30, [], 65],
  ['destinations-ios', 100, 30, ['3', ']'], 65],
  ['schemes-packages', 100, 30, ['2', ']'], 65],
  ['filter-typing', 100, 30, ['2', '/', 'k', 'i', 't'], 65],
  ['help', 100, 30, ['?'], 65],
  ['expanded', 100, 30, ['3', '+'], 65],
  ['build-failed', 100, 30, ['b', 3.5], 65],
  ['build-failed-issues', 100, 30, ['b', 3.5, '0', 'i'], 65],
  ['build-succeeded', 100, 30, ['b', 3.5], 0],
  ['run-attached', 100, 30, ['r', 5], 0],
  ['run-attached-timestamps', 100, 30, ['r', 5, 's'], 0],
  ['run-attached-build-row', 100, 30, ['r', 5, '4', 'j'], 0],
  ['run-stopped', 100, 30, ['r', 5, 'x', 1.5], 0],
  ['run-failed-build', 100, 30, ['r', 3.5], 65],
]

def check_tui
  pass = 0
  SEQUENCES.each do |name, w, h, keys, build_exit|
    dir = tmpdir; bin, work, env = tui_env(dir, build_exit: build_exit)
    got = capture(bin, BIN, work, env, w, h, keys)
    if REF
      rdir = tmpdir; rbin, rwork, renv = tui_env(rdir, build_exit: build_exit)
      want = capture(rbin, REF, rwork, renv, w, h, keys)
      got_m = got.gsub(work, '<work>'); want_m = want.gsub(rwork, '<work>')
      ok = got_m == want_m
      puts "  #{ok ? 'PASS' : 'FAIL'} tui.#{name}"
      show('screen', want_m, got_m) unless ok
      pass += 1 if ok
      FileUtils.rm_rf(rdir)
    else
      puts "=== tui.#{name} ===\n#{got}\n"
    end
    FileUtils.rm_rf(dir)
  end
  puts "tui: #{pass}/#{SEQUENCES.size} match the reference" if REF
  !REF || pass == SEQUENCES.size
end

ok = true
ok &= check_cli if %w[cli all].include?(MODE)
ok &= check_tui if %w[tui all].include?(MODE)
exit(ok ? 0 : 1)
