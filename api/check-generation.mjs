import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { parse } from 'yaml'

const contract = parse(await readFile(new URL('./openapi.yaml', import.meta.url), 'utf8'))
const generated = await readFile(new URL('./generated/api.d.ts', import.meta.url), 'utf8')
const goModels = await readFile(new URL('../backend/internal/platform/httpx/apimodel/apimodel.gen.go', import.meta.url), 'utf8')

for (const [path, pathItem] of Object.entries(contract.paths)) {
  assert.ok(generated.includes(JSON.stringify(path)), `TypeScript output missing ${path}`)
  for (const [method, operation] of Object.entries(pathItem)) {
    if (!operation || typeof operation !== 'object' || !operation.operationId) continue
    for (const output of [generated]) assert.ok(output.includes(operation.operationId), `TypeScript output missing ${operation.operationId}`)
    for (const [status, response] of Object.entries(operation.responses ?? {})) {
      const responseSchema = response.content?.['application/json']?.schema ?? response.content?.['application/problem+json']?.schema ?? Object.values(response.content ?? {}).map(media => media.schema).find(schema => schema?.$ref)
      if (responseSchema?.$ref) {
        const name = responseSchema.$ref.split('/').at(-1)
        const start = generated.indexOf(`${operation.operationId}:`)
        const operationBlock = generated.slice(start, generated.indexOf('\n    };', start))
        assert.ok(operationBlock.includes(`            ${status}:`) || operationBlock.includes(`            "${status}":`), `${path} ${method} TS response missing HTTP ${status}`)
        assert.ok(generated.includes(`components["schemas"]["${name}"]`), `${path} ${method} TS response missing ${name}`)
      }
      for (const [header, spec] of Object.entries(response.headers ?? {})) if (spec.$ref) assert.ok(contract.components.headers[spec.$ref.split('/').at(-1)], `${path} ${status} has unresolved header ref`)
    }
    if (operation.requestBody) {
      const schema = operation.requestBody.content?.['application/json']?.schema
      assert.ok(schema?.$ref && generated.includes(`components["schemas"]["${schema.$ref.split('/').at(-1)}"]`), `${operation.operationId}: missing generated request model`)
      assert.ok(operation.requestBody.content['application/json'].example || Object.keys(operation.requestBody.content['application/json'].examples ?? {}).length, `${operation.operationId}: missing request example`)
    }
  }
}
for (const [name, schema] of Object.entries(contract.components.schemas)) {
  assert.ok(generated.includes(`${name}:`), `TypeScript output missing schema ${name}`)
  for (const field of Object.keys(schema.properties ?? {})) assert.ok(generated.includes(field), `TypeScript output missing field ${name}.${field}`)
  for (const value of schema.properties?.status?.enum ?? []) assert.ok(generated.includes(JSON.stringify(value)), `TypeScript output missing status value ${value}`)
  assert.ok(goModels.includes(name), `Generated Go missing model ${name}`)
}
for (const name of ['SetupRequest', 'SetupStatus', 'Problem', 'ValidationProblem', 'LoginRequest', 'Session']) assert.ok(goModels.includes(`type ${name} struct`), `Generated Go missing ${name}`)
assert.ok(/type Session struct \{[^}]*DefaultHouseholdId \*string/s.test(goModels), 'Generated Go Session must have optional DefaultHouseholdId')
// oapi-codegen v2.8.0 no longer emits SessionCookieScopes/SetupTokenScopes. Those constants never enforced
// authentication; enforcement is verified against this contract by backend/cmd/tendo/security_contract_test.go.
assert.deepEqual(Object.keys(contract.components.securitySchemes).sort(), ['SessionCookie', 'SetupToken'], 'Contract security schemes changed; update security_contract_test.go')
for (const [path, pathItem] of Object.entries(contract.paths)) {
  for (const [method, operation] of Object.entries(pathItem)) {
    if (!operation || typeof operation !== 'object' || !operation.operationId) continue
    assert.ok(Array.isArray(operation.security), `${method.toUpperCase()} ${path} must declare security explicitly`)
    assert.ok(operation.security.length <= 1 && operation.security.every(requirement => Object.keys(requirement).length === 1 && contract.components.securitySchemes[Object.keys(requirement)[0]]), `${method.toUpperCase()} ${path} must declare [] or exactly one known security scheme`)
  }
}
// Required nullable response properties stay single pointers and always serialize.
// The shared model file contains every schema formerly checked in the five byte-identical outputs.
const isNullable = property => (Array.isArray(property.type) && property.type.includes('null')) || (property.oneOf ?? []).some(branch => branch.type === 'null')
for (const [name, schema] of Object.entries(contract.components.schemas)) {
  for (const [field, property] of Object.entries(schema.properties ?? {})) {
    if (!isNullable(property) || !(schema.required ?? []).includes(field) || property.readOnly) continue
    const struct = goModels.match(new RegExp(`type ${name} struct \\{[^}]*\\}`, 's'))?.[0]
    assert.ok(struct, `${name}: generated Go missing struct`)
    const goField = field[0].toUpperCase() + field.slice(1)
    assert.match(struct, new RegExp('\\n\\s*' + goField + '\\s+\\*[A-Za-z][\\w.]*\\s+`json:"' + field + '"`'), `${name}.${field} must be a single-pointer, always-serialized nullable field`)
  }
}
for (const operationId of ['createInvitation','listInvitations','revokeInvitation','acceptInvitationWithNewAccount','acceptInvitation','listMembers','createSession','getSession','deleteSession']) assert.ok(generated.includes(`${operationId}:`), `TypeScript output missing operation ${operationId}`)
assert.ok(/Session: \{[^}]*defaultHouseholdId\?: string/s.test(generated), 'TypeScript Session must have optional defaultHouseholdId')
assert.ok(/LoginRequest: \{[^}]*login: string[^}]*password: string/s.test(generated), 'TypeScript LoginRequest missing required fields')
for (const name of ['StatusResponse', 'Problem']) assert.ok(goModels.includes(`type ${name} struct`), `Generated Go missing ${name}`)
for (const name of ['Invitation','InvitationCreated','InvitationList','Member','MemberList','Membership','InvitedAccount','AcceptInvitationRequest','AcceptInvitationNewAccountRequest']) assert.ok(goModels.includes(`type ${name} struct`), `Generated Go missing ${name}`)
assert.ok(/type Membership struct \{[^}]*HouseholdId string[^}]*Role/s.test(goModels), 'Membership must omit login')
assert.ok(/type InvitedAccount struct \{[^}]*HouseholdId string[^}]*Login\s+string/s.test(goModels), 'InvitedAccount must include login')
const tsMembership = generated.slice(generated.indexOf('Membership: {'), generated.indexOf('\n        };', generated.indexOf('Membership: {')))
const tsInvitedAccount = generated.slice(generated.indexOf('InvitedAccount: {'), generated.indexOf('\n        };', generated.indexOf('InvitedAccount: {')))
assert.ok(tsMembership.includes('householdId: string') && tsMembership.includes('role: "member"') && !tsMembership.includes('login:'), 'TS Membership must omit login')
assert.ok(tsInvitedAccount.includes('login: string') && tsInvitedAccount.includes('householdId: string'), 'TS InvitedAccount must include login')
assert.ok(/type Household struct \{[^}]*CreatedAt time\.Time[^}]*Id\s+string[^}]*Name\s+string[^}]*Timezone\s+string/s.test(goModels), 'Generated Go missing Household model with required fields')
assert.ok(!/Version/.test(goModels.match(/type Household struct \{[^}]*\}/s)?.[0] ?? ''), 'Household response model must not expose the internal version')
assert.ok(generated.includes('getHousehold:'), 'TypeScript output missing operation getHousehold')
assert.ok(/Household: \{[^}]*id: string[^}]*name: string[^}]*timezone: string[^}]*createdAt: string/s.test(generated), 'TypeScript Household missing required fields')
for (const name of ['Subject', 'SubjectList', 'CreateSubjectRequest', 'UpdateSubjectRequest']) if (name !== 'UpdateSubjectRequest') assert.ok(goModels.includes(`type ${name} struct`), `Generated Go missing ${name}`)
assert.ok(/type Subject struct \{[^}]*Archived\s+bool[^}]*CreatedAt\s+time\.Time[^}]*Id\s+string[^}]*LinkedUserId\s+\*string\s+`json:\"linkedUserId\"`[^}]*Name\s+string[^}]*Type\s+SubjectType[^}]*UpdatedAt\s+time\.Time/s.test(goModels), 'Generated Go missing Subject model with required fields')
assert.ok(!/Version|HouseholdId/.test(goModels.match(/type Subject struct \{[^}]*\}/s)?.[0] ?? ''), 'Subject response model must not expose version or householdId')
assert.ok(/type SubjectList struct \{[^}]*Items \[\]Subject[^}]*NextCursor \*string `json:"nextCursor"`/s.test(goModels), 'Generated SubjectList must always serialize nextCursor')
assert.ok(/type UpdateSubjectRequest struct \{[^}]*Archived \*bool[^}]*Name \*string[^}]*Type \*SubjectType/s.test(goModels), 'Generated UpdateSubjectRequest must have optional fields')
for (const type of ['person', 'home', 'vehicle', 'pet', 'custom']) assert.ok(goModels.includes(`"${type}"`), `Generated Go missing subject type ${type}`)
for (const operationId of ['createSubject', 'listSubjects', 'getSubject', 'updateSubject']) assert.ok(generated.includes(`${operationId}:`), `TypeScript output missing operation ${operationId}`)
assert.ok(/Subject: \{[^}]*id: string[^}]*type: components\["schemas"\]\["SubjectType"\][^}]*name: string[^}]*archived: boolean[^}]*createdAt: string[^}]*updatedAt: string/s.test(generated), 'TypeScript Subject missing required fields')
assert.ok(/SubjectList: \{[^}]*nextCursor: string \| null/s.test(generated), 'TypeScript SubjectList must have required nullable nextCursor')
assert.ok(/SubjectType: "person" \| "home" \| "vehicle" \| "pet" \| "custom"/.test(generated), 'TypeScript SubjectType enum mismatch')
