import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { parse } from 'yaml'

const contract = parse(await readFile(new URL('./openapi.yaml', import.meta.url), 'utf8'))
const generated = await readFile(new URL('./generated/api.d.ts', import.meta.url), 'utf8')
const generatedHealth = await readFile(new URL('./generated/health.d.ts', import.meta.url), 'utf8')
const goSetup = await readFile(new URL('../backend/internal/identity/httpapi/setup.gen.go', import.meta.url), 'utf8')
const goHealth = await readFile(new URL('../backend/internal/platform/httpx/health.gen.go', import.meta.url), 'utf8')
const goHousehold = await readFile(new URL('../backend/internal/household/httpapi/household.gen.go', import.meta.url), 'utf8')
const goSubject = await readFile(new URL('../backend/internal/subject/httpapi/subject.gen.go', import.meta.url), 'utf8')

for (const [path, pathItem] of Object.entries(contract.paths)) {
  assert.ok(generated.includes(JSON.stringify(path)), `TypeScript output missing ${path}`)
  for (const [method, operation] of Object.entries(pathItem)) {
    if (!operation || typeof operation !== 'object' || !operation.operationId) continue
    for (const output of [generated, generatedHealth]) assert.ok(output.includes(operation.operationId), `TypeScript output missing ${operation.operationId}`)
    for (const [status, response] of Object.entries(operation.responses ?? {})) {
      const responseSchema = response.content?.['application/json']?.schema ?? response.content?.['application/problem+json']?.schema
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
  assert.ok(goSetup.includes(name) || goHealth.includes(name) || goHousehold.includes(name) || goSubject.includes(name), `Generated Go missing model ${name}`)
}
for (const name of ['SetupRequest', 'SetupStatus', 'Problem', 'ValidationProblem', 'LoginRequest', 'Session']) assert.ok(goSetup.includes(`type ${name} struct`), `Generated setup Go missing ${name}`)
assert.ok(/type Session struct \{[^}]*DefaultHouseholdId \*string/s.test(goSetup), 'Generated Go Session must have optional DefaultHouseholdId')
assert.ok(goSetup.includes('SessionCookieScopes'), 'Generated Go missing SessionCookie security scheme')
for (const operationId of ['createInvitation','listInvitations','revokeInvitation','acceptInvitationWithNewAccount','acceptInvitation','listMembers','createSession','getSession','deleteSession']) assert.ok(generated.includes(`${operationId}:`), `TypeScript output missing operation ${operationId}`)
assert.ok(/Session: \{[^}]*defaultHouseholdId\?: string/s.test(generated), 'TypeScript Session must have optional defaultHouseholdId')
assert.ok(/LoginRequest: \{[^}]*login: string[^}]*password: string/s.test(generated), 'TypeScript LoginRequest missing required fields')
for (const name of ['StatusResponse', 'Problem']) assert.ok(goHealth.includes(`type ${name} struct`), `Generated health Go missing ${name}`)
for (const name of ['Invitation','InvitationCreated','InvitationList','Member','MemberList','Membership','InvitedAccount','AcceptInvitationRequest','AcceptInvitationNewAccountRequest']) assert.ok(goSetup.includes(`type ${name} struct`) || goHousehold.includes(`type ${name} struct`), `Generated Go missing ${name}`)
assert.ok(/type Membership struct \{[^}]*HouseholdId string[^}]*Role/s.test(goSetup), 'Membership must omit login')
assert.ok(/type InvitedAccount struct \{[^}]*HouseholdId string[^}]*Login\s+string/s.test(goSetup), 'InvitedAccount must include login')
const tsMembership = generated.slice(generated.indexOf('Membership: {'), generated.indexOf('\n        };', generated.indexOf('Membership: {')))
const tsInvitedAccount = generated.slice(generated.indexOf('InvitedAccount: {'), generated.indexOf('\n        };', generated.indexOf('InvitedAccount: {')))
assert.ok(tsMembership.includes('householdId: string') && tsMembership.includes('role: "member"') && !tsMembership.includes('login:'), 'TS Membership must omit login')
assert.ok(tsInvitedAccount.includes('login: string') && tsInvitedAccount.includes('householdId: string'), 'TS InvitedAccount must include login')
assert.ok(/type Household struct \{[^}]*CreatedAt time\.Time[^}]*Id\s+string[^}]*Name\s+string[^}]*Timezone\s+string/s.test(goHousehold), 'Generated household Go missing Household model with required fields')
assert.ok(!/Version/.test(goHousehold.match(/type Household struct \{[^}]*\}/s)?.[0] ?? ''), 'Household response model must not expose the internal version')
assert.ok(generated.includes('getHousehold:'), 'TypeScript output missing operation getHousehold')
assert.ok(/Household: \{[^}]*id: string[^}]*name: string[^}]*timezone: string[^}]*createdAt: string/s.test(generated), 'TypeScript Household missing required fields')
for (const name of ['Subject', 'SubjectList', 'CreateSubjectRequest', 'UpdateSubjectRequest']) if (name !== 'UpdateSubjectRequest') assert.ok(goSubject.includes(`type ${name} struct`), `Generated subject Go missing ${name}`)
assert.ok(/type Subject struct \{[^}]*Archived\s+bool[^}]*CreatedAt time\.Time[^}]*Id\s+string[^}]*Name\s+string[^}]*Type\s+SubjectType[^}]*UpdatedAt time\.Time/s.test(goSubject), 'Generated subject Go missing Subject model with required fields')
assert.ok(!/Version|HouseholdId/.test(goSubject.match(/type Subject struct \{[^}]*\}/s)?.[0] ?? ''), 'Subject response model must not expose version or householdId')
assert.ok(/type SubjectList struct \{[^}]*Items \[\]Subject[^}]*NextCursor \*string `json:"nextCursor"`/s.test(goSubject), 'Generated SubjectList must always serialize nextCursor')
assert.ok(/type UpdateSubjectRequest struct \{[^}]*Archived \*bool[^}]*Name \*string[^}]*Type \*SubjectType/s.test(goSubject), 'Generated UpdateSubjectRequest must have optional fields')
for (const type of ['person', 'home', 'vehicle', 'pet', 'custom']) assert.ok(goSubject.includes(`"${type}"`), `Generated Go missing subject type ${type}`)
for (const operationId of ['createSubject', 'listSubjects', 'getSubject', 'updateSubject']) assert.ok(generated.includes(`${operationId}:`), `TypeScript output missing operation ${operationId}`)
assert.ok(/Subject: \{[^}]*id: string[^}]*type: components\["schemas"\]\["SubjectType"\][^}]*name: string[^}]*archived: boolean[^}]*createdAt: string[^}]*updatedAt: string/s.test(generated), 'TypeScript Subject missing required fields')
assert.ok(/SubjectList: \{[^}]*nextCursor: string \| null/s.test(generated), 'TypeScript SubjectList must have required nullable nextCursor')
assert.ok(/SubjectType: "person" \| "home" \| "vehicle" \| "pet" \| "custom"/.test(generated), 'TypeScript SubjectType enum mismatch')
