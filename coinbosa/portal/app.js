/* ═══════════════════════════════════════════════════════════════════════════
   PORTAIL DE MIGRATION — le script de la page.

   Il vit dans un fichier separe, et non dans la page, parce que la politique de
   securite du site interdit tout script en ligne (script-src 'self'). Un
   <script> ecrit dans le HTML serait SILENCIEUSEMENT bloque le jour ou cette
   page serait publiee : elle s afficherait normalement, la validation d adresse
   ne ferait rien, et rien dans l interface ne dirait pourquoi. Le livre blanc
   avait exactement le meme besoin, d ou whitepaper/app.js.

   La validation est entierement locale. Aucune donnee ne quitte le navigateur.
   ═══════════════════════════════════════════════════════════════════════════ */

// Validation entièrement côté client. Aucune donnée n'est transmise : ce squelette
// attend un service sécurisé (voir docs/MIGRATION.md).

// Adresses de dépôt : VIDES tant qu'elles ne sont pas officiellement publiées.
// Ne jamais coder ici une adresse non vérifiée — ce serait un vecteur de détournement.
const DEPOSIT = { solana: '' };

const $ = (id) => document.getElementById(id);

// --- validation EIP-55 de l'adresse de destination, sans dépendance externe ---
// keccak-256 minimal (implémentation compacte, suffisante pour la somme de contrôle)
function keccak256(bytes) {
  const RC = [0x0000000000000001n,0x0000000000008082n,0x800000000000808An,0x8000000080008000n,
    0x000000000000808Bn,0x0000000080000001n,0x8000000080008081n,0x8000000000008009n,
    0x000000000000008An,0x0000000000000088n,0x0000000080008009n,0x000000008000000An,
    0x000000008000808Bn,0x800000000000008Bn,0x8000000000008089n,0x8000000000008003n,
    0x8000000000008002n,0x8000000000000080n,0x000000000000800An,0x800000008000000An,
    0x8000000080008081n,0x8000000000008080n,0x0000000080000001n,0x8000000080008008n];
  const R = [0,1,62,28,27,36,44,6,55,20,3,10,43,25,39,41,45,15,21,8,18,2,61,56,14];
  const M = (1n<<64n)-1n;
  const rot = (x,n)=>((x<<BigInt(n))|(x>>(64n-BigInt(n))))&M;
  let A = new Array(25).fill(0n);
  const rate = 136;
  const pad = [...bytes, 0x01];
  while (pad.length % rate !== 0) pad.push(0);
  pad[pad.length-1] |= 0x80;
  for (let off=0; off<pad.length; off+=rate) {
    for (let i=0;i<rate/8;i++){ let l=0n; for(let j=7;j>=0;j--) l=(l<<8n)|BigInt(pad[off+i*8+j]); A[i]^=l; }
    for (let r=0;r<24;r++){
      const C=[0n,0n,0n,0n,0n];
      for(let x=0;x<5;x++) C[x]=A[x]^A[x+5]^A[x+10]^A[x+15]^A[x+20];
      const D=[0n,0n,0n,0n,0n];
      for(let x=0;x<5;x++) D[x]=C[(x+4)%5]^rot(C[(x+1)%5],1);
      for(let x=0;x<5;x++) for(let y=0;y<25;y+=5) A[x+y]^=D[x];
      let B=new Array(25).fill(0n);
      for(let x=0;x<5;x++) for(let y=0;y<5;y++) B[y+((2*x+3*y)%5)*5]=rot(A[x+y*5],R[x+y*5]);
      for(let x=0;x<5;x++) for(let y=0;y<5;y++) A[x+y*5]=B[x+y*5]^((~B[(x+1)%5+y*5])&B[(x+2)%5+y*5]);
      A[0]^=RC[r];
    }
  }
  const out=[];
  for(let i=0;i<32;i++) out.push(Number((A[Math.floor(i/8)]>>BigInt((i%8)*8))&0xffn));
  return out;
}
function isChecksumAddress(addr) {
  if (!/^0x[0-9a-fA-F]{40}$/.test(addr)) return false;
  const body = addr.slice(2);
  const lower = body.toLowerCase();
  const hash = keccak256([...lower].map(c=>c.charCodeAt(0)));
  const hex = hash.map(b=>b.toString(16).padStart(2,'0')).join('');
  for (let i=0;i<40;i++){
    const c = body[i];
    if (!/[a-fA-F]/.test(c)) continue;
    const up = parseInt(hex[i],16) >= 8;
    if ((up && c !== c.toUpperCase()) || (!up && c !== c.toLowerCase())) return false;
  }
  return true;
}

function validate() {
  let ok = true;
  const setHint = (id, msg, cls) => { const h=$('h-'+id); if(h){h.textContent=msg||''; h.className='hint'+(cls?' '+cls:'');} };

  const prenom = $('prenom').value.trim();
  const nom = $('nom').value.trim();
  if (!prenom) ok=false;
  if (!nom) ok=false;

  const reseau = $('reseau').value;
  if (!reseau) ok=false;
  $('deposit').hidden = !reseau;
  if (reseau) {
    $('reseau-nom').textContent = '(Solana)';
    $('deposit-addr').textContent = DEPOSIT[reseau] || 'à publier avant l’ouverture du portail';
  }

  const dest = $('dest').value.trim();
  const destEl = $('dest');
  if (!dest) { setHint('dest','Le BOSA natif y sera crédité. Vérifiez chaque caractère.'); destEl.className='mono'; ok=false; }
  else if (!/^0x[0-9a-fA-F]{40}$/.test(dest)) { setHint('dest','Format d’adresse invalide.','err'); destEl.className='mono bad'; ok=false; }
  else if (dest === dest.toLowerCase() || dest === dest.toUpperCase()) { setHint('dest','Adresse sans casse : impossible de vérifier la somme de contrôle. Recopiez l’adresse telle qu’elle apparaît dans votre portefeuille.','err'); destEl.className='mono bad'; ok=false; }
  else if (!isChecksumAddress(dest)) { setHint('dest','Somme de contrôle EIP-55 invalide : cette adresse comporte une erreur.','err'); destEl.className='mono bad'; ok=false; }
  else { setHint('dest','Adresse valide.','ok'); destEl.className='mono good'; }

  const tx = $('tx').value.trim();
  if (!tx) { setHint('tx',''); ok=false; }
  else if (!/^[1-9A-HJ-NP-Za-km-z]{43,88}$/.test(tx)) { setHint('tx','Empreinte Solana invalide (signature base58).','err'); ok=false; }
  else setHint('tx','Format reconnu.','ok');

  $('submit').disabled = !ok;
  return ok;
}

for (const id of ['prenom','nom','reseau','dest','tx']) {
  $(id).addEventListener('input', validate);
  $(id).addEventListener('change', validate);
}

$('f').addEventListener('submit', (e) => {
  e.preventDefault();
  if (!validate()) return;
  // Aperçu : aucune transmission. Le portail réel appellerait ici un service sécurisé.
  alert('Aperçu non fonctionnel.\n\nLe portail réel vérifierait votre dépôt sur le réseau d’origine, ' +
        'créditerait le BOSA à votre adresse, et vous remettrait l’empreinte de la transaction Coinbosa Chain comme preuve.\n\n' +
        'Voir docs/MIGRATION.md.');
});
