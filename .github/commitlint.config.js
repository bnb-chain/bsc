const validateTypeNums = (parsedCommit) => {
    const mergePrefix = "Merge pull request"
    if (parsedCommit.raw.startsWith(mergePrefix)) {
        console.log('this is a merge commit:' + parsedCommit.raw) 
        return [true,'']
    }

    if (!parsedCommit.type) {
        return [false, 'invalid commit message, should be like "name: descriptions.", yours: "' + parsedCommit.raw + '"']
    }

    const types = parsedCommit.type.split(' ')
    for (var i=0;i<types.length;i++){
        if ((types[i].toLowerCase() == "wip") || (types[i].toLowerCase() == "r4r")) {
            return [false, 'R4R  or WIP is not acceptable, no matter upper case or lower case']
        }
    }
     return [true,'']
  }


module.exports = {
  parserPreset: {
    parserOpts: {
      headerPattern: /^(.*):.*/,
    }
  },
  extends: ['@commitlint/config-conventional'],
  plugins: ['commitlint-plugin-function-rules'],
  rules: {
    'subject-empty':[2, 'always'],
    'scope-empty':[2, 'always'],
    'type-enum': [2, 'never'],
    'type-case': [0, 'always'],
    'function-rules/type-case': [2, 'always', validateTypeNums],
    'header-max-length': [
      2,
      'always',
      80,
    ],
  },
  helpUrl:
    'https://github.com/bnb-chain/bsc/tree/develop/docs/lint/commit.md',
}
