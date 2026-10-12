'use strict';

const readline = require('node:readline');

function isInteractive() {
  return Boolean(process.stdin.isTTY && process.stdout.isTTY);
}

function select(question, options) {
  const fallback = options[0];

  if (options.length === 1 || !isInteractive()) {
    process.stdout.write(`${question} ${fallback.label}\n`);
    return Promise.resolve(fallback.value);
  }

  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
  });

  return new Promise((resolve) => {
    const list = options.map((opt, i) => `  ${i + 1}) ${opt.label}`).join('\n');
    process.stdout.write(`${question}\n${list}\n`);

    const ask = () => {
      rl.question(`Select [1-${options.length}] (1): `, (answer) => {
        const trimmed = answer.trim();
        if (trimmed === '') {
          rl.close();
          resolve(fallback.value);
          return;
        }
        const index = Number.parseInt(trimmed, 10);
        if (Number.isInteger(index) && index >= 1 && index <= options.length) {
          rl.close();
          resolve(options[index - 1].value);
          return;
        }
        process.stdout.write('Invalid selection, please try again.\n');
        ask();
      });
    };
    ask();
  });
}

module.exports = { select, isInteractive };
