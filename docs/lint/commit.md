## Commit Format Requirements
1. The head line should contain no more than 72 characters
2. head line is composed by  scope : subject 
3. multi-scope is supported, separated by a space, such as scope1 scope2 scope3: subject 
   it is better that scope number =<3 , scope length <= 20 char , but not mandatory
4. keyword, such as  R4R  or WIP is not acceptable in the scope, no matter upper case or lower case.

#### Example: Single Scope
```
evm: optimize opcode mload
```

#### Example: Multi Scope
```
rpc core db: refactor the interface of trie access
```

#### Example: Big Scope
if the change is too big, impact several scopes, the scope name can be bep, feat or fix
```
bep130: implement parallel evm

feat: implement parallel trie prefetch

fix: stack overflow on GetCommitState
```
