import { readFile, readdir, lstat } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
const root=fileURLToPath(new URL('../dist/',import.meta.url));
const review=JSON.parse(await readFile(new URL('../../docs/contracts/scalar-release-review.json',import.meta.url),'utf8'));
const approval=JSON.parse(await readFile(new URL('../../assets/brand/documentation-brand.json',import.meta.url),'utf8'));
if(approval.status!=='approved-for-documentation')throw Error('Documentation visual approval missing');
if(review.publicationProfile!=='static-reference-only')throw Error('Public profile must be explicitly selected');
async function walk(dir,prefix=''){
 for(const name of await readdir(dir)){
  const rel=prefix+name;
  if(/(?:^|\/)(?:api-viewer|scalar-candidate|@scalar)(?:\/|$)/i.test(rel))throw Error('Unreviewed viewer distribution prohibited: '+rel);
  const full=join(dir,name), st=await lstat(full);
  if(st.isDirectory())await walk(full,rel+'/');
  else if(!st.isFile()||st.isSymbolicLink())throw Error('Unexpected public artifact file: '+rel);
 }
}
await walk(root);
const html=await readFile(join(root,'reference/api/index.html'),'utf8');
if(html.includes('/ruxbase/api-viewer/'))throw Error('Public reference links to excluded viewer');
await readFile(join(root,'openapi.json'));
console.log('Public scope verified: static reference and OpenAPI only; no Scalar viewer distributed. Brand approved.');
