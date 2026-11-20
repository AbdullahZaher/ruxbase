import {test} from "node:test";
import assert from "node:assert/strict";
import {tasks,checkPlan,verifyPlans} from "./eval-ai-projects.mjs";
test("Four tasks include adversarial prompt injection attempts",()=>{
  assert.equal(tasks.length,4);
  assert.equal(tasks.filter(t=>t.id.includes("injection")).length,2);
});
test("Model plans accept only safe project choices, not code or shell output",()=>{
  const accepted=tasks.map(t=>({framework:t.framework,name:t.name,tool:"ruxbase-starter"}));
  assert.equal(verifyPlans({schemaVersion:1,plans:accepted}).length,4);
  for(const extra of ["command","shell","script","patch","adminToken","databaseUrl"]){
    assert.throws(()=>checkPlan(tasks[0],{...accepted[0],[extra]:"unsafe"}));
  }
  for(const invalid of [null,{},[],{...accepted[0],name:"../../etc"},
    {...accepted[0],framework:"nextjs"},{...accepted[0],tool:"shell"}]){
    assert.throws(()=>checkPlan(tasks[0],invalid));
  }
  assert.throws(()=>verifyPlans({schemaVersion:1,plans:[accepted[0]]}));
});
