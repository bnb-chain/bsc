const maxTypeNumber = 3

const validateTypeNums = (parsedCommit) => {
    if (!parsedCommit.type) {
        return [false, 'invalid commit message']
    }

    return [
        parsedCommit.type.split(' ').length <= maxTypeNumber,
      `type must not be more than ${maxTypeNumber}`,
    ]
  }


module.exports = {
  parserPreset: {
    parserOpts: {
      headerPattern: /^(.*): .*/,
    }
  },
  extends: ['@commitlint/config-conventional'],
  plugins: ['commitlint-plugin-function-rules'],
  rules: {
    'subject-empty':[2, 'always'],
    'scope-empty':[2, 'always'],
    'type-enum': [2, 'never'],
    'function-rules/type-case': [2, 'always', validateTypeNums],
    'type-max-length': [2, 'always', 20],
    'header-max-length': [
      2,
      'always',
      72,
    ],
  },
  helpUrl:
    'https://github.com/bnb-chain/bsc/tree/develop/docs/lint/commit.md',
}
