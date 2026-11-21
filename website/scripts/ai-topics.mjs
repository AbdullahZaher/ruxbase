export const TOPICS = [
    { name: 'llms-integrations.txt', title: 'SDK and integration guides', prefixes: ['integrations/', 'tutorials/'] },
    { name: 'llms-concepts.txt', title: 'Core concepts', prefixes: ['concepts/'] },
    { name: 'llms-api.txt', title: 'HTTP and machine references', prefixes: ['reference/'] },
    { name: 'llms-extensions.txt', title: 'Server extensions', prefixes: ['extend-go/', 'extend-js/'] },
    { name: 'llms-operations.txt', title: 'PostgreSQL and operations', prefixes: ['postgresql/', 'operations/', 'data/', 'authorization/', 'guides/'] },
];
export const introFor = identity => '# Ruxbase documentation\n\n> PostgreSQL-native self-hosted backend platform. Development documentation, not a certified production release.\n\nSource commit: ' + identity.sourceCommit + '\n\n';
export const topicPages = (pages, topic) => pages.filter(p => topic.prefixes.some(prefix=>p.target.startsWith(prefix)));
export function makeTopic(identity, topic, contents) {
    if (!contents.length) throw new Error('Empty topic: '+topic.name);
    const text=introFor(identity)+'## '+topic.title+'\n\n'+contents.join('\n---\n\n');
    if (Buffer.byteLength(text)>262144) throw new Error('Topic context exceeds 256 KiB: '+topic.name);
    return text;
}
