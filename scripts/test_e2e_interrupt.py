#!/usr/bin/env python3
"""Real SIGTERM regression through public e2e-smoke wrapper, runner, and PG owner."""
from __future__ import annotations
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parent.parent
APP = r'''#!/usr/bin/env python3
import os,signal,subprocess,sys
from http.server import BaseHTTPRequestHandler,HTTPServer
if len(sys.argv)>1 and sys.argv[1]=="migrate": raise SystemExit(0)
marker=os.environ["INTERRUPT_MARKERS"]
with open(marker+"/app.pid","w") as f:f.write(str(os.getpid()))
w=subprocess.Popen([sys.executable,"-c","import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(120)"])
with open(marker+"/app-worker.pid","w") as f:f.write(str(w.pid))
class H(BaseHTTPRequestHandler):
 def do_GET(self):self.send_response(200);self.end_headers();self.wfile.write(b'{"status":"ok"}')
 def log_message(self,*a):pass
h,p=os.environ["TENDO_LISTEN_ADDR"].rsplit(":",1);HTTPServer((h,int(p)),H).serve_forever()
'''
PROVIDER = r'''#!/usr/bin/env python3
import os,signal,subprocess,sys
from http.server import BaseHTTPRequestHandler,HTTPServer
marker=os.environ["INTERRUPT_MARKERS"]
with open(marker+"/provider.pid","w") as f:f.write(str(os.getpid()))
w=subprocess.Popen([sys.executable,"-c","import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(120)"])
with open(marker+"/provider-worker.pid","w") as f:f.write(str(w.pid))
class H(BaseHTTPRequestHandler):
 def do_GET(self):self.send_response(200);self.end_headers();self.wfile.write(b'ok')
 def log_message(self,*a):pass
HTTPServer(("127.0.0.1",int(os.environ["LISTEN_ADDR"].rsplit(":",1)[1])),H).serve_forever()
'''

class E2EWrapperInterruptTests(unittest.TestCase):
 def test_public_wrapper_sigterm_stops_groups_cleans_pg_and_restores_existing_dist(self):
  admin=os.environ.get("TENDO_TEST_POSTGRES_ADMIN_URL")
  if not admin: raise RuntimeError("TENDO_TEST_POSTGRES_ADMIN_URL required for live E2E cleanup regression")
  import importlib.util
  spec=importlib.util.spec_from_file_location("pg_interrupt_probe",ROOT/"scripts/postgres-test-service.py")
  helper=importlib.util.module_from_spec(spec);spec.loader.exec_module(helper);probe=helper.Service(admin)
  roles_before=set(filter(None,probe.run("SELECT rolname FROM pg_roles WHERE rolname LIKE 'tendo_m_%' OR rolname LIKE 'tendo_r_%'",tuples=True).splitlines()))
  dbs_before=set(filter(None,probe.run("SELECT datname FROM pg_database WHERE datname LIKE 'tendo_db_%'",tuples=True).splitlines()))
  with tempfile.TemporaryDirectory(prefix="tendo-e2e-wrapper-test-") as td:
   tmp=Path(td); repo=tmp/"repo"; (repo/"scripts").mkdir(parents=True)
   (repo/"frontend/node_modules/.bin").mkdir(parents=True)
   (repo/"frontend/package.json").write_text('{"scripts":{"build":"mkdir -p ../backend/internal/platform/webui/dist/assets && echo new > ../backend/internal/platform/webui/dist/index.html && echo new > ../backend/internal/platform/webui/dist/assets/new.js"}}')
   (repo/"frontend/dist").mkdir(parents=True); (repo/"frontend/dist/.gitkeep").write_text(""); (repo/"frontend/dist/old.txt").write_text("old frontend")
   embedded=repo/"backend/internal/platform/webui/dist"; (embedded/"assets").mkdir(parents=True)
   (embedded/"index.html").write_text("old embedded index"); (embedded/"assets/old.js").write_text("old embedded asset")
   for name in ("e2e-smoke.sh","e2e-native-runner.py","postgres-test-service.py"):
    import shutil; shutil.copy2(ROOT/"scripts"/name,repo/"scripts"/name)
   pkg=repo/"frontend/node_modules/@playwright/test/package.json"; pkg.parent.mkdir(parents=True); pkg.write_text('{"version":"1.64.0"}')
   pw=repo/"frontend/node_modules/.bin/playwright"; pw.write_text("#!/usr/bin/env bash\nexit 0\n"); pw.chmod(0o755)
   bindir=tmp/"bin";bindir.mkdir()
   go=bindir/"go";go.write_text("#!/usr/bin/env python3\nimport os,sys\na=sys.argv[1:];out=a[a.index('-o')+1];src=os.environ['E2E_FAKE_APP'] if 'oidc-provider' not in a[-1] else os.environ['E2E_FAKE_PROVIDER'];open(out,'w').write(open(src).read());os.chmod(out,0o755)\n");go.chmod(0o755)
   app=tmp/"fake-app.py";app.write_text(APP);app.chmod(0o755)
   provider=tmp/"fake-provider.py";provider.write_text(PROVIDER);provider.chmod(0o755)
   markers=tmp/"markers";markers.mkdir()
   env=os.environ.copy();env.update({"PATH":str(bindir)+os.pathsep+env["PATH"],"E2E_FAKE_APP":str(app),"E2E_FAKE_PROVIDER":str(provider),"INTERRUPT_MARKERS":str(markers),"TENDO_TEST_POSTGRES_ADMIN_URL":admin})
   shell=subprocess.Popen(["bash",str(repo/"scripts/e2e-smoke.sh")],cwd=repo,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,start_new_session=True)
   try:
    expected=("app.pid","app-worker.pid","provider.pid","provider-worker.pid")
    deadline=time.monotonic()+45
    while not all((markers/n).exists() for n in expected) and time.monotonic()<deadline and shell.poll() is None:time.sleep(.05)
    if shell.poll() is not None:
     output=shell.stdout.read() if shell.stdout else "";self.fail(f"public wrapper exited before owned children started ({shell.returncode}): {output}")
    self.assertTrue(all((markers/n).exists() for n in expected),"public wrapper never reached live app/provider children")
    pids=[int((markers/n).read_text()) for n in expected]
    os.kill(shell.pid,signal.SIGTERM);shell.wait(timeout=45)
    output=shell.stdout.read() if shell.stdout else ""
    self.assertNotEqual(shell.returncode,0,output);self.assertNotIn(admin,output)
    for pid in pids:
     try:os.kill(pid,0)
     except ProcessLookupError:continue
     try:state=Path(f"/proc/{pid}/stat").read_text().split()[2]
     except (FileNotFoundError,ProcessLookupError):continue
     self.assertEqual(state,"Z",f"owned E2E PID {pid} remains alive")
    self.assertEqual((repo/"frontend/dist/.gitkeep").read_text(),"")
    self.assertEqual((repo/"frontend/dist/old.txt").read_text(),"old frontend")
    self.assertEqual((embedded/"index.html").read_text(),"old embedded index")
    self.assertEqual((embedded/"assets/old.js").read_text(),"old embedded asset")
   finally:
    if shell.poll() is None:
     try:os.killpg(shell.pid,signal.SIGTERM)
     except ProcessLookupError:pass
     try:shell.wait(timeout=10)
     except subprocess.TimeoutExpired:
      try:os.killpg(shell.pid,signal.SIGKILL)
      except ProcessLookupError:pass
      shell.wait()
    if shell.stdout:shell.stdout.close()
  roles_after=set(filter(None,probe.run("SELECT rolname FROM pg_roles WHERE rolname LIKE 'tendo_m_%' OR rolname LIKE 'tendo_r_%'",tuples=True).splitlines()))
  dbs_after=set(filter(None,probe.run("SELECT datname FROM pg_database WHERE datname LIKE 'tendo_db_%'",tuples=True).splitlines()))
  self.assertEqual(roles_after,roles_before,"public shell SIGTERM leaked per-run roles")
  self.assertEqual(dbs_after,dbs_before,"public shell SIGTERM leaked per-run databases")

if __name__=="__main__":unittest.main(verbosity=2)
