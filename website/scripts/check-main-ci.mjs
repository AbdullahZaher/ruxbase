const required=['ci','docs-foundation','docs-brand-review','docs-quickstart','docs-build-candidate','docs-performance','docs-starters'];
const repo=process.env.GITHUB_REPOSITORY,sha=process.env.GITHUB_SHA,token=process.env.GH_TOKEN;
if(repo!=='AbdullahZaher/ruxbase'||process.env.GITHUB_REF!=='refs/heads/main'||!token||!/^[0-9a-f]{40}$/.test(sha))throw Error('Invalid main deployment context');
const endpoint=new URL('https://api.github.com/repos/'+repo+'/actions/runs');
endpoint.search=new URLSearchParams({head_sha:sha,branch:'main',event:'push',per_page:'100'});
const response=await fetch(endpoint,{headers:{Authorization:'Bearer '+token,Accept:'application/vnd.github+json','X-GitHub-Api-Version':'2022-11-28'},redirect:'error',signal:AbortSignal.timeout(15000)});
if(!response.ok)throw Error('Unable to verify main CI: HTTP '+response.status);
const data=await response.json();
if(data.total_count>100)throw Error('Too many runs to verify safely');
for(const name of required){
 const runs=data.workflow_runs.filter(r=>r.name===name&&r.head_sha===sha&&r.head_branch==='main'&&r.event==='push').sort((a,b)=>b.id-a.id);
 const run=runs[0];
 if(!run||run.status!=='completed'||run.conclusion!=='success')throw Error('Required exact-main workflow not green: '+name);
 console.log('Exact main CI verified:',name,run.id,run.run_attempt);
}
