import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
const assert=(v,m)=>{if(!v)throw Error(m);};
const stop=new Set('a an the is are be to of in on for and or what which how do does can i it its with from when why by as at this that all any'.split(' '));
function tokens(s){return (s.toLowerCase().match(/[a-z0-9_]+/g)||[]).filter(w=>!stop.has(w));}
function normalize(s){return s.replace(/\s+/g,' ').trim().toLowerCase();}
function body(s){return s.replace(/^> (?:Development documentation|Source commit:|Canonical page:).*$/gm,'').replace(/\]\(https?:\/\/[^)]+\)/g,']').replace(/^\[View as Markdown\].*$/gm,'');}
export function rankPages(query,pages){
 const q=[...new Set(tokens(query))];
 const docs=pages.map(p=>{const words=tokens(body(p.text));const counts=new Map();for(const w of words)counts.set(w,(counts.get(w)||0)+1);return {...p,counts,length:words.length,titleWords:new Set(tokens(p.title))};});
 const avg=docs.reduce((sum,d)=>sum+d.length,0)/docs.length;
 return docs.map(d=>{let score=0;for(const w of q){const tf=d.counts.get(w)||0,df=docs.filter(x=>x.counts.has(w)).length;const idf=Math.log(1+(docs.length-df+.5)/(df+.5));if(tf)score+=idf*tf*2.2/(tf+1.2*(.25+.75*d.length/avg));if(d.titleWords.has(w))score+=idf*1.5;}return {target:d.target,score};}).sort((a,b)=>b.score-a.score||(a.target<b.target?-1:1));
}
export async function evaluateBenchmark(root,fixture,expectedCount=30,minimumPassed=27){
 const m=JSON.parse(await readFile(join(root,'documentation-manifest.json'),'utf8'));
 const pages=await Promise.all(m.pages.map(async p=>{const text=await readFile(join(root,p.target),'utf8');assert(createHash('sha256').update(text).digest('hex')===p.sha256,'Markdown hash mismatch');return {target:p.target,title:p.title,text};}));
 assert(fixture.questions.length===expectedCount&&new Set(fixture.questions.map(q=>q.id)).size===expectedCount,'Expected '+expectedCount+' unique questions');
 const results=fixture.questions.map(q=>{
  const page=pages.find(p=>p.target===q.target);assert(page,'Unknown expected source '+q.target);
  const ranked=rankPages(q.query,pages);const rank=ranked.findIndex(p=>p.target===q.target)+1;
  const missingEvidence=q.evidence.filter(e=>!normalize(page.text).includes(normalize(e)));
  return {id:q.id,query:q.query,critical:q.critical,expected:q.target,rank,top3:ranked.slice(0,3).map(x=>x.target),missingEvidence,pass:rank>0&&rank<=3&&!missingEvidence.length};
 });
 const passed=results.filter(r=>r.pass).length,criticalFailures=results.filter(r=>r.critical&&!r.pass).length;
 return {schemaVersion:1,sourceCommit:m.sourceCommit,sourceTree:m.sourceTree,method:fixture.scope,pageCount:pages.length,total:results.length,passed,criticalFailures,hitAt1:results.filter(r=>r.rank===1).length,hitAt3:results.filter(r=>r.rank<=3&&r.rank>0).length,passedGate:passed>=minimumPassed&&criticalFailures===0,results};
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){
 const root=fileURLToPath(new URL('../dist/',import.meta.url));
 const fixture=JSON.parse(await readFile(new URL('../../docs/benchmarks/ai-retrieval-questions.json',import.meta.url),'utf8'));
 const result=await evaluateBenchmark(root,fixture);
 const integrationFixture=JSON.parse(await readFile(new URL('../../docs/benchmarks/integration-retrieval-questions.json',import.meta.url),'utf8'));
 const integrations=await evaluateBenchmark(root,integrationFixture,12,11);
 const developerFixture=JSON.parse(await readFile(new URL('../../docs/benchmarks/developer-retrieval-questions.json',import.meta.url),'utf8'));
 const developer=await evaluateBenchmark(root,developerFixture,18,17);
 const out=fileURLToPath(new URL('../browser-evidence/',import.meta.url));await mkdir(out,{recursive:true});await writeFile(join(out,'ai-retrieval.json'),JSON.stringify(result,null,2)+'\n');
 console.log('AI retrieval proxy: '+result.passed+'/'+result.total+' passed; top1 '+result.hitAt1+'; top3 '+result.hitAt3+'; critical failures '+result.criticalFailures);
 for(const x of result.results.filter(x=>!x.pass))console.log('Retrieval miss:',x.id,'rank',x.rank,'missing evidence',JSON.stringify(x.missingEvidence));
 await writeFile(join(out,'integration-retrieval.json'),JSON.stringify(integrations,null,2)+'\n');
 console.log('Integration retrieval proxy: '+integrations.passed+'/'+integrations.total+' passed; top1 '+integrations.hitAt1+'; top3 '+integrations.hitAt3+'; critical failures '+integrations.criticalFailures);
 for(const x of integrations.results.filter(x=>!x.pass))console.log('Integration retrieval miss:',x.id,'rank',x.rank,'missing evidence',JSON.stringify(x.missingEvidence));
 await writeFile(join(out,'developer-retrieval.json'),JSON.stringify(developer,null,2)+'\n');
 console.log('Developer retrieval proxy: '+developer.passed+'/'+developer.total+' passed; top1 '+developer.hitAt1+'; top3 '+developer.hitAt3+'; critical failures '+developer.criticalFailures);
 for(const x of developer.results.filter(x=>!x.pass))console.log('Developer retrieval miss:',x.id,'rank',x.rank,'missing evidence',JSON.stringify(x.missingEvidence));
 assert(developer.passedGate,'Developer retrieval gate failed (>=17/18 and zero critical failures)');
 assert(result.passedGate,'Baseline retrieval acceptance gate failed (>=27/30 and zero critical failures)');
 assert(integrations.passedGate,'Integration retrieval acceptance gate failed (>=11/12 and zero critical failures)');
}
