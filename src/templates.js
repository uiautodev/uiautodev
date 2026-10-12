'use strict';

const TEMPLATES = [
  {
    name: 'preact',
    label: 'Preact + TypeScript + Tailwind CSS',
    description: 'Plugin template with built-in AI development guidance',
    repo: 'uiautodev-plugins/preact-template',
    branch: 'master',
  },
];

function buildTarballUrl({ repo, branch }) {
  return `https://codeload.github.com/${repo}/tar.gz/refs/heads/${branch}`;
}

function listTemplates() {
  return TEMPLATES.map((template) => ({
    ...template,
    tarball: buildTarballUrl(template),
  }));
}

function getTemplate(name) {
  const template = listTemplates().find((item) => item.name === name);
  if (!template) {
    const available = TEMPLATES.map((item) => item.name).join(', ');
    throw new Error(
      `Unknown template "${name}". Available templates: ${available}`
    );
  }
  return template;
}

module.exports = { listTemplates, getTemplate, buildTarballUrl };
