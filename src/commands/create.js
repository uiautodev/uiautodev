'use strict';

const path = require('node:path');

const { listTemplates, getTemplate } = require('../templates');
const { select } = require('../prompt');
const { scaffold } = require('../scaffold');

async function resolveTemplate(name) {
  if (name) {
    return getTemplate(name);
  }
  const choice = await select(
    'Select a template:',
    listTemplates().map((template) => ({
      label: `${template.name} - ${template.label}`,
      value: template.name,
    }))
  );
  return getTemplate(choice);
}

async function createCommand(opts) {
  const template = await resolveTemplate(opts.template);
  const targetDir = await scaffold({
    template,
    name: opts.name,
    force: opts.force,
  });

  const rel = path.relative(process.cwd(), targetDir) || opts.name;
  process.stdout.write(
    `\nCreated ${template.name} plugin at ${rel}\n\n` +
      'Next steps:\n' +
      `  cd ${rel}\n` +
      '  npm install\n' +
      '  npm run dev\n'
  );
}

module.exports = createCommand;
