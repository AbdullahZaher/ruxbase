import { readFile } from 'node:fs/promises';
const review=JSON.parse(await readFile(new URL('../../docs/contracts/scalar-release-review.json',import.meta.url),'utf8'));
const provenance=JSON.parse(await readFile(new URL('../dist/api-viewer/provenance.json',import.meta.url),'utf8'));
if(review.package!==provenance.package||review.version!==provenance.version||review.integrity!==provenance.integrity)throw Error('Scalar review identity mismatch');
if(review.status!=='approved'||!review.reviewedEvidence||!review.reviewedEvidence.length)throw Error('Scalar distribution review remains open; public deployment blocked');
console.log('Scalar distribution review gate: approved identity and recorded evidence');
