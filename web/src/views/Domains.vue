<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { toast } from '../ui'
import { isAdmin } from '../auth'
import Modal from '../components/Modal.vue'

const PROVIDER_LABEL = { tencent: '腾讯云', aliyun: '阿里云', volc: '火山引擎' }

const router = useRouter()
const rows = ref([])
const loading = ref(false)
const showAdd = ref(false)
const domainName = ref('')
const remark = ref('')

const showVerify = ref(false)
const verifyReport = ref([])
const verifyDomainId = ref(0)

const showBind = ref(false)
const bindTarget = ref(null)
const accounts = ref([])
const bindings = ref([])

async function openBind(d) {
  bindTarget.value = d
  try {
    const [accs, binds] = await Promise.all([
      api.get('/api/accounts'),
      api.get(`/api/domains/${d.id}/accounts`)
    ])
    accounts.value = accs
    bindings.value = binds
  } catch (e) {
    toast(e.message, 'err')
    return
  }
  showBind.value = true
}

function isBound(accId) {
  return bindings.value.some((b) => b.account_id === accId)
}

async function doBind(acc) {
  if (binding.value) return
  binding.value = true
  try {
    const r = await api.post(`/api/domains/${bindTarget.value.id}/bind`, { account_id: acc.id })
    if (r.verify_status === 'ok') toast('已绑定并验证通过', 'ok')
    else toast(`已绑定，但验证失败：${r.verify_message || ''}`, 'err')
    bindings.value = await api.get(`/api/domains/${bindTarget.value.id}/accounts`)
    load()
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    binding.value = false
  }
}

const unbindTarget = ref(null)
const binding = ref(false)

async function doUnbind(acc) {
  unbindTarget.value = acc
}

async function confirmUnbind() {
  const acc = unbindTarget.value
  if (!acc || binding.value) return
  binding.value = true
  try {
    await api.post(`/api/domains/${bindTarget.value.id}/unbind`, { account_id: acc.id })
    toast('已解绑', 'ok')
    unbindTarget.value = null
    bindings.value = await api.get(`/api/domains/${bindTarget.value.id}/accounts`)
    load()
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    binding.value = false
  }
}

async function load() {
  loading.value = true
  try {
    rows.value = await api.get('/api/domains')
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

function providers(d) {
  return (d.providers || '').split(',').filter(Boolean).map((p) => {
    const [name, on] = p.split(':')
    const lbl = PROVIDER_LABEL[name] || name
    return { name, on: on !== 'off', lbl }
  })
}

async function addDomain() {
  try {
    await api.post('/api/domains', { name: domainName.value, remark: remark.value })
    toast('域名已添加', 'ok')
    showAdd.value = false
    domainName.value = ''
    remark.value = ''
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function verify(d) {
  toast('校验中…')
  try {
    verifyReport.value = await api.post(`/api/domains/${d.id}/verify`)
    verifyDomainId.value = d.id
    showVerify.value = true
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function repair(rep, ids) {
  try {
    const res = await api.post(`/api/domains/${verifyDomainId.value}/repair`, {
      account_id: rep.account_id,
      record_ids: ids
    })
    const fails = (res.sync || []).filter((s) => s.status === 'failed')
    if (fails.length) toast(`推送完成，${fails.length} 条失败（将自动重试）: ${fails[0].message || ''}`, 'err')
    else toast('已全部推送成功', 'ok')
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function delDomain(d) {
  if (!confirm('删除域名及其全部本地解析记录（不影响云平台）?')) return
  try {
    await api.del(`/api/domains/${d.id}`)
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

function openRecords(d) {
  router.push({ path: '/records', query: { domainId: d.id, domainName: d.name } })
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-head">
      <h2 class="page-title">域名管理</h2>
      <button class="btn ghost sm" :disabled="loading" @click="load">刷新</button>
    </div>
    <div class="toolbar">
      <button class="btn primary" @click="showAdd = true">添加域名</button>
      <span class="spacer"></span>
      <span class="hint">点击行进入解析记录管理</span>
    </div>
    <div class="panel scroll">
      <table>
        <thead><tr><th>ID</th><th>域名</th><th>同步平台</th><th>备注</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="d in rows" :key="d.id" :data-id="d.id" @click="openRecords(d)">
            <td>{{ d.id }}</td>
            <td>{{ d.name }}</td>
            <td>
              <span v-for="p in providers(d)" :key="p.name" class="badge" :class="[p.name, { dim: !p.on }]"
                    :title="p.lbl + (p.on ? '' : '（已暂停同步）')">{{ p.lbl }}{{ p.on ? '' : ' 暂停' }}</span>
              <span v-if="!providers(d).length" class="badge neutral">未绑定</span>
            </td>
            <td>{{ d.remark }}</td>
            <td class="ops">
              <button class="btn ghost sm" @click.stop="verify(d)">一致性校验</button>
              <button class="btn ghost sm" @click.stop="openBind(d)">绑定账号</button>
              <button v-if="isAdmin()" class="btn ghost sm danger" @click.stop="delDomain(d)">删除</button>
            </td>
          </tr>
          <tr v-if="!rows.length"><td colspan="5" class="empty">暂无域名</td></tr>
        </tbody>
      </table>
    </div>

    <Modal v-if="showBind" :title="`绑定/解绑账号 - ${bindTarget?.name || ''}`" @close="showBind = false">
      <p class="hint">已绑定的账号会参与该域名的同步；解绑会清理该平台下此域名的队列与映射。</p>
      <div class="panel scroll" style="margin-top:10px">
        <table>
          <thead><tr><th>厂商</th><th>名称</th><th>状态</th><th>操作</th></tr></thead>
          <tbody>
            <tr v-for="a in accounts" :key="a.id">
              <td><span class="badge" :class="a.provider">{{ PROVIDER_LABEL[a.provider] || a.provider }}</span></td>
              <td>{{ a.name }}</td>
              <td>
                <span v-if="isBound(a.id)" class="badge ok">已绑定</span>
                <span v-else class="badge neutral">未绑定</span>
              </td>
              <td class="ops">
                <button v-if="isBound(a.id)" class="btn danger sm" @click="doUnbind(a)">解绑</button>
                <button v-else class="btn primary sm" @click="doBind(a)">绑定</button>
              </td>
            </tr>
            <tr v-if="!accounts.length"><td colspan="4" class="empty">暂无云账号，请先到「云账号」添加</td></tr>
          </tbody>
        </table>
      </div>
      <template #actions>
        <button class="btn" @click="showBind = false">关闭</button>
      </template>
    </Modal>

    <Modal v-if="unbindTarget" title="确认解绑" @close="unbindTarget = null">
      <p>确定要解绑账号 <b>{{ unbindTarget.name }}</b> 吗？</p>
      <p class="hint" style="margin-top:8px">解绑后：</p>
      <ul class="hint" style="margin:4px 0 0 18px;line-height:1.8">
        <li>该平台将不再参与此域名的同步</li>
        <li>该平台下此域名的同步队列与远端映射会被清理</li>
        <li>不会删除云平台上的已有记录</li>
      </ul>
      <template #actions>
        <button class="btn" @click="unbindTarget = null">取消</button>
        <button class="btn danger" @click="confirmUnbind">确认解绑</button>
      </template>
    </Modal>

    <Modal v-if="showAdd" title="添加域名（自动在各云厂商验证）" @close="showAdd = false">
      <div class="field"><label>域名</label><input v-model="domainName" placeholder="example.com"></div>
      <div class="field"><label>备注</label><input v-model="remark"></div>
      <template #actions>
        <button class="btn" @click="showAdd = false">取消</button>
        <button class="btn primary" @click="addDomain">添加</button>
      </template>
    </Modal>

    <Modal v-if="showVerify" title="一致性校验结果" wide @close="showVerify = false">
      <div class="panel scroll">
        <table>
          <thead><tr><th>平台</th><th>状态</th><th>本地</th><th>远端</th><th>本地缺失</th><th>远端多余</th></tr></thead>
          <tbody>
            <template v-for="rep in verifyReport" :key="rep.account_id + rep.provider">
              <tr>
                <td>{{ PROVIDER_LABEL[rep.provider] || rep.provider || '-' }}</td>
                <td>
                  <span class="badge" :class="rep.status">{{ rep.status === 'consistent' ? '一致' : rep.status === 'inconsistent' ? '不一致' : '错误: ' + (rep.message || '') }}</span>
                </td>
                <td>{{ rep.local_count ?? '-' }}</td>
                <td>{{ rep.remote_count ?? '-' }}</td>
                <td>{{ rep.missing_local ?? '-' }}</td>
                <td>{{ rep.extra_remote ?? '-' }}</td>
              </tr>
              <tr v-if="rep.detail?.missing?.length">
                <td colspan="6" class="detail-cell">
                  {{ PROVIDER_LABEL[rep.provider] || rep.provider }} 缺少的记录：<br>
                  <span v-for="m in rep.detail.missing" :key="m.id">· {{ m.key }}<br></span>
                  <button class="btn primary sm" @click="repair(rep, rep.detail.missing.map(m => m.id))">
                    推送这 {{ rep.detail.missing.length }} 条到该平台
                  </button>
                </td>
              </tr>
              <tr v-if="rep.detail?.extra?.length">
                <td colspan="6" class="detail-cell">该平台多出的记录（本地没有，可点"从云平台拉取"导入）：{{ rep.detail.extra.join('、') }}</td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
      <template #actions>
        <button class="btn primary" @click="showVerify = false">关闭</button>
      </template>
    </Modal>
  </div>
</template>
