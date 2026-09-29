import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mentionQuery, messageRoute } from '../src/mentions.ts';

test('mention picker recognises standalone @ at caret and leaves emails alone', () => {
  assert.deepEqual(mentionQuery('@评审',3), {start:0,end:3,query:'评审'});
  assert.deepEqual(mentionQuery('请看看 @评审 后文',7), {start:4,end:7,query:'评审'});
  assert.equal(mentionQuery('test@example.com',16),null);
  assert.deepEqual(mentionQuery('请@评审',4), {start:1,end:4,query:'评审'});
  assert.equal(mentionQuery('@评审\n补充',6),null);
  assert.deepEqual(mentionQuery('@John Smith',11), {start:0,end:11,query:'John Smith'});
});

test('a selected agent changes just this message; cleared selection uses the room mode', () => {
  for (const mode of ['lead','discussion']) {
    assert.deepEqual(messageRoute('group',mode,'reviewer'), {action:'mention',agentIDs:['reviewer']});
    assert.deepEqual(messageRoute('group',mode,''), {action:mode,agentIDs:[]});
  }
  assert.deepEqual(messageRoute('private','lead',''),{action:'direct',agentIDs:[]});
});
