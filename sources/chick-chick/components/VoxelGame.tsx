

import React, { useState, useRef, useEffect, useMemo, useCallback } from 'react';
import { useFrame, useThree, extend } from '@react-three/fiber';
import { OrthographicCamera, Float, Html, shaderMaterial } from '@react-three/drei';
import * as THREE from 'three';
import { LaneData, LaneType, GameStatus, Skin, DeathReason } from '../types';
import { MOVEMENT_SPEED, JUMP_HEIGHT, BIOMES, STATIC_COLORS, SKINS, PLAYABLE_WIDTH, VISIBLE_WIDTH } from '../constants';

// Create a single AudioContext instance to avoid warnings
let audioContext: AudioContext | null = null;
const getAudioContext = () => {
    if (!audioContext) {
        const AudioContextClass = window.AudioContext || (window as any).webkitAudioContext;
        if (!AudioContextClass) return null;
        audioContext = new AudioContextClass();
    }
    return audioContext;
};

const playSound = (type: 'jump' | 'coin' | 'die' | 'train' | 'bonk' | 'flattened' | 'drowned' | 'smashed' | 'lost', pitch: number = 1.0) => {
    const ctx = getAudioContext();
    if (!ctx) return;

    // Resume context if suspended (for browser autoplay policies)
    if (ctx.state === 'suspended') {
        ctx.resume().catch(() => {});
    }

    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.connect(gain);
    gain.connect(ctx.destination);
    const now = ctx.currentTime;

    if (type === 'jump') {
        osc.type = 'square';
        osc.frequency.setValueAtTime(150, now);
        osc.frequency.exponentialRampToValueAtTime(300, now + 0.1);
        gain.gain.setValueAtTime(0.05, now);
        gain.gain.exponentialRampToValueAtTime(0.01, now + 0.1);
        osc.start(now);
        osc.stop(now + 0.1);
    } else if (type === 'coin') {
        osc.type = 'sine';
        osc.frequency.setValueAtTime(1200, now);
        osc.frequency.setValueAtTime(1600, now + 0.1);
        gain.gain.setValueAtTime(0.05, now);
        gain.gain.exponentialRampToValueAtTime(0.01, now + 0.3);
        osc.start(now);
        osc.stop(now + 0.3);
    } else if (type === 'die') {
        // Generic die sound (fallback)
        osc.type = 'sawtooth';
        osc.frequency.setValueAtTime(100, now);
        osc.frequency.exponentialRampToValueAtTime(20, now + 0.5);
        gain.gain.setValueAtTime(0.1, now);
        gain.gain.exponentialRampToValueAtTime(0.01, now + 0.5);
        osc.start(now);
        osc.stop(now + 0.5);
    } else if (type === 'train') {
        osc.type = 'triangle';
        osc.frequency.setValueAtTime(80, now);
        gain.gain.setValueAtTime(0.0, now);
        gain.gain.linearRampToValueAtTime(0.1, now + 0.2);
        gain.gain.linearRampToValueAtTime(0.0, now + 0.6);
        osc.start(now);
        osc.stop(now + 0.6);
    } else if (type === 'bonk') {
        // Bonk sound: short, low thud with pitch variation
        // pitch: 1.0 = base (rock/low tree), 1.25 = medium tree, 1.5 = tall tree
        osc.type = 'sine';
        osc.frequency.setValueAtTime(200 * pitch, now);
        osc.frequency.exponentialRampToValueAtTime(80 * pitch, now + 0.15);
        gain.gain.setValueAtTime(0.15, now);
        gain.gain.exponentialRampToValueAtTime(0.01, now + 0.15);
        osc.start(now);
        osc.stop(now + 0.15);
    } else if (type === 'flattened') {
        // Car hit: Squish sound with harsh impact
        osc.type = 'square';
        osc.frequency.setValueAtTime(250, now);
        osc.frequency.exponentialRampToValueAtTime(50, now + 0.3);
        gain.gain.setValueAtTime(0.12, now);
        gain.gain.linearRampToValueAtTime(0.08, now + 0.1);
        gain.gain.exponentialRampToValueAtTime(0.01, now + 0.3);
        osc.start(now);
        osc.stop(now + 0.3);
    } else if (type === 'drowned') {
        // Drowning: Bubbling, gurgling descent
        osc.type = 'sine';
        osc.frequency.setValueAtTime(400, now);
        osc.frequency.exponentialRampToValueAtTime(100, now + 0.6);
        // Create bubbling effect with gain modulation
        gain.gain.setValueAtTime(0.08, now);
        gain.gain.linearRampToValueAtTime(0.04, now + 0.15);
        gain.gain.linearRampToValueAtTime(0.08, now + 0.3);
        gain.gain.linearRampToValueAtTime(0.04, now + 0.45);
        gain.gain.exponentialRampToValueAtTime(0.01, now + 0.6);
        osc.start(now);
        osc.stop(now + 0.6);
    } else if (type === 'smashed') {
        // Train impact: Heavy metallic crash
        osc.type = 'sawtooth';
        osc.frequency.setValueAtTime(180, now);
        osc.frequency.exponentialRampToValueAtTime(30, now + 0.4);
        gain.gain.setValueAtTime(0.15, now);
        gain.gain.exponentialRampToValueAtTime(0.01, now + 0.4);
        osc.start(now);
        osc.stop(now + 0.4);
    } else if (type === 'lost') {
        // Lost in void: Eerie fade out
        osc.type = 'triangle';
        osc.frequency.setValueAtTime(600, now);
        osc.frequency.exponentialRampToValueAtTime(200, now + 0.8);
        gain.gain.setValueAtTime(0.1, now);
        gain.gain.exponentialRampToValueAtTime(0.01, now + 0.8);
        osc.start(now);
        osc.stop(now + 0.8);
    }
};

interface ExtendedLaneData extends LaneData {
  obstacles: Set<number>;
  coins: Set<number>;
  sceneryLeft: number[];
  sceneryRight: number[];
  carSpeed?: number;
  logSpeed?: number;
  carCount?: number;
  logCount?: number;
  logOffsets?: number[]; 
  biomeIndex: number;
}

// --- SHADERS & VFX ---

const HexBoundaryMaterial = shaderMaterial(
  { uColor: new THREE.Color(1, 0, 0), uOpacity: 0, uTime: 0 },
  // Vertex Shader
  `
    varying vec2 vUv;
    varying vec3 vPosition;
    void main() {
      vUv = uv;
      vPosition = position;
      gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
    }
  `,
  // Fragment Shader
  `
    uniform vec3 uColor;
    uniform float uOpacity;
    uniform float uTime;
    varying vec2 vUv;
    varying vec3 vPosition;

    // Hexagon SDF
    float hexDist(vec2 p) {
        p = abs(p);
        float c = dot(p, normalize(vec2(1.0, 1.73)));
        c = max(c, p.x);
        return c;
    }

    void main() {
        if (uOpacity <= 0.01) discard;

        // Create hex grid
        vec2 scale = vec2(10.0, 10.0);
        vec2 grid = vUv * scale;
        // Offset rows
        grid.x += step(1.0, mod(grid.y, 2.0)) * 0.5;
        
        vec2 f = fract(grid);
        vec2 p = f - 0.5;
        
        // Correct aspect for flat hexagons
        p.x *= 1.1547; 
        
        float d = hexDist(p);
        
        // Edge glow
        float glow = smoothstep(0.45, 0.5, d) - smoothstep(0.5, 0.55, d);
        
        // Fill opacity based on proximity to edge
        float pattern = glow + 0.1;

        // Pulse effect
        float pulse = (sin(uTime * 5.0 + vUv.y * 10.0) * 0.5 + 0.5) * 0.5;
        
        float alpha = pattern * uOpacity * (1.0 + pulse);
        
        gl_FragColor = vec4(uColor, alpha);
    }
  `
);

extend({ HexBoundaryMaterial });

const BoundaryWalls: React.FC<{ playerPos: THREE.Vector3, limit: number }> = ({ playerPos, limit }) => {
    const materialRefLeft = useRef<any>(null);
    const materialRefRight = useRef<any>(null);
    const groupRef = useRef<THREE.Group>(null);

    useFrame((state) => {
        if (groupRef.current) {
            groupRef.current.position.z = playerPos.z;
        }

        const distLeft = Math.abs(playerPos.x - (-limit));
        const distRight = Math.abs(playerPos.x - limit);
        
        // Calculate opacity based on distance (closer = more opaque)
        // Start showing at dist < 3.5
        const maxDist = 4.0;
        
        const opLeft = Math.max(0, 1.0 - (distLeft / maxDist));
        const opRight = Math.max(0, 1.0 - (distRight / maxDist));

        if (materialRefLeft.current) {
            materialRefLeft.current.uOpacity = opLeft * 0.8; // Max 0.8 opacity
            materialRefLeft.current.uTime = state.clock.elapsedTime;
        }
        if (materialRefRight.current) {
            materialRefRight.current.uOpacity = opRight * 0.8;
            materialRefRight.current.uTime = state.clock.elapsedTime;
        }
    });

    const height = 15;
    const length = 40;

    return (
        <group ref={groupRef}>
            {/* Left Wall */}
            <mesh position={[-limit - 0.1, height/2 - 2, 5]} rotation={[0, Math.PI/2, 0]}>
                <planeGeometry args={[length, height]} />
                {/* @ts-ignore */}
                <hexBoundaryMaterial ref={materialRefLeft} transparent depthWrite={false} side={THREE.DoubleSide} />
            </mesh>
            {/* Right Wall */}
            <mesh position={[limit + 0.1, height/2 - 2, 5]} rotation={[0, -Math.PI/2, 0]}>
                 <planeGeometry args={[length, height]} />
                 {/* @ts-ignore */}
                <hexBoundaryMaterial ref={materialRefRight} transparent depthWrite={false} side={THREE.DoubleSide} />
            </mesh>
        </group>
    )
}

const WeatherSystem: React.FC<{ biomeName: string, playerPos: THREE.Vector3 }> = ({ biomeName, playerPos }) => {
    const count = 100;
    const meshRef = useRef<THREE.InstancedMesh>(null);
    const particles = useMemo(() => {
        const temp = [];
        for(let i=0; i<count; i++) {
            temp.push({
                pos: new THREE.Vector3((Math.random()-0.5)*40, Math.random()*20, (Math.random()-0.5)*40),
                speed: Math.random() * 0.1 + 0.05
            });
        }
        return temp;
    }, []);
    const dummy = useMemo(() => new THREE.Object3D(), []);

    useFrame(() => {
        if(!meshRef.current) return;
        particles.forEach((p, i) => {
            p.pos.y -= p.speed;
            if(p.pos.y < 0) p.pos.y = 20;
            const renderX = playerPos.x + (p.pos.x); 
            const renderZ = playerPos.z + (p.pos.z);
            dummy.position.set(renderX, p.pos.y, renderZ);
            dummy.updateMatrix();
            meshRef.current!.setMatrixAt(i, dummy.matrix);
        });
        meshRef.current.instanceMatrix.needsUpdate = true;
    });

    if (biomeName === 'DESERT' || biomeName === 'INFERNAL') return null; 

    const color = biomeName === 'SNOW' ? '#FFF' : '#3498DB';
    const scale = biomeName === 'SNOW' ? 0.15 : 0.08;

    return (
        <instancedMesh ref={meshRef} args={[undefined, undefined, count]}>
            <boxGeometry args={[scale, scale, scale]} />
            <meshBasicMaterial color={color} transparent opacity={0.6} />
        </instancedMesh>
    );
};

const PlayerParticles: React.FC<{ isMoving: boolean, position: THREE.Vector3, skinId: string }> = ({ isMoving, position, skinId }) => {
    const particlesRef = useRef<{pos: THREE.Vector3, life: number, vel: THREE.Vector3}[]>([]);
    const meshRef = useRef<THREE.InstancedMesh>(null);
    const dummy = useMemo(() => new THREE.Object3D(), []);

    useFrame((_, delta) => {
        if (isMoving && Math.random() < 0.3) {
            particlesRef.current.push({
                pos: position.clone().add(new THREE.Vector3((Math.random()-0.5)*0.5, 0, (Math.random()-0.5)*0.5)),
                life: 1.0,
                vel: new THREE.Vector3((Math.random()-0.5)*2, Math.random()*2, (Math.random()-0.5)*2)
            });
        }

        for (let i = particlesRef.current.length - 1; i >= 0; i--) {
            const p = particlesRef.current[i];
            p.life -= delta * 2;
            p.pos.add(p.vel.clone().multiplyScalar(delta));
            if (p.life <= 0) particlesRef.current.splice(i, 1);
        }

        if (meshRef.current) {
            meshRef.current.count = particlesRef.current.length;
            particlesRef.current.forEach((p, i) => {
                dummy.position.copy(p.pos);
                dummy.scale.setScalar(p.life * 0.2);
                dummy.updateMatrix();
                meshRef.current!.setMatrixAt(i, dummy.matrix);
            });
            meshRef.current.instanceMatrix.needsUpdate = true;
        }
    });

    let color = '#FFF';
    if (skinId === 'robot') color = '#FFFF00'; 
    if (skinId === 'zombie') color = '#76FF03'; 
    if (skinId === 'vampire') color = '#333'; 
    if (skinId === 'golden') color = '#FFD700'; 

    return (
        <instancedMesh ref={meshRef} args={[undefined, undefined, 50]}>
            <boxGeometry args={[1,1,1]} />
            <meshBasicMaterial color={color} />
        </instancedMesh>
    )
}

// --- GAME OBJECTS ---

const Coin: React.FC<{ position: [number, number, number], collected: boolean }> = ({ position, collected }) => {
    if (collected) return null;
    return (
        <group position={position}>
            <Float speed={5} rotationIntensity={0} floatIntensity={0.5}>
                <mesh castShadow rotation={[0, Math.PI/4, Math.PI/2]} position={[0, 0.5, 0]}>
                    <cylinderGeometry args={[0.25, 0.25, 0.1, 8]} />
                    <meshStandardMaterial color={STATIC_COLORS.COIN} metalness={0.8} roughness={0.2} />
                </mesh>
            </Float>
        </group>
    )
}

const Player: React.FC<{ position: THREE.Vector3, isMoving: boolean, direction: number, jumpProgress: number, skinId: string }> = ({ position, isMoving, direction, jumpProgress, skinId }) => {
  const groupRef = useRef<THREE.Group>(null);
  const skin = SKINS.find(s => s.id === skinId) || SKINS[0];
  
  const visualPosition = useMemo(() => {
    const pos = position.clone();
    if (isMoving) pos.y += Math.sin(jumpProgress * Math.PI) * JUMP_HEIGHT;
    return pos;
  }, [position, isMoving, jumpProgress]);

  const targetRotation = useMemo(() => {
      if (direction === 1) return Math.PI / 2;
      if (direction === 2) return Math.PI;
      if (direction === 3) return -Math.PI / 2;
      return 0;
  }, [direction]);

  const scale = useMemo(() => {
      if (!isMoving) return [1, 1, 1];
      const t = jumpProgress;
      let sy = 1;
      let sxz = 1;
      if (t < 0.2) { sy = 0.8 + t; sxz = 1.1 - t * 0.5;} 
      else if (t > 0.2 && t < 0.8) { sy = 1.2; sxz = 0.8; } 
      else { sy = 0.8 + (1-t); sxz = 1.1 - (1-t) * 0.5; }
      return [sxz, sy, sxz] as [number, number, number];
  }, [isMoving, jumpProgress]);

  useFrame(() => {
      if (groupRef.current) {
          groupRef.current.rotation.y = THREE.MathUtils.lerp(groupRef.current.rotation.y, targetRotation, 0.3);
          groupRef.current.scale.set(scale[0], scale[1], scale[2]);
      }
  });

  const renderModel = () => {
      switch(skin.model) {
          case 'chicken':
              return (
                  <group>
                      <mesh position={[0, 0.3, 0]} castShadow><boxGeometry args={[0.5, 0.5, 0.5]} /><meshStandardMaterial color="#FFF" /></mesh>
                      <mesh position={[0, 0.55, 0.2]} castShadow><boxGeometry args={[0.1, 0.1, 0.2]} /><meshStandardMaterial color="#FF3333" /></mesh> {/* Comb */}
                      <mesh position={[0, 0.3, 0.3]} castShadow><boxGeometry args={[0.15, 0.15, 0.1]} /><meshStandardMaterial color="#FFA000" /></mesh> {/* Beak */}
                      <mesh position={[0.1, 0.4, 0.26]}><boxGeometry args={[0.05, 0.05, 0.05]} /><meshStandardMaterial color="#000" /></mesh>
                      <mesh position={[-0.1, 0.4, 0.26]}><boxGeometry args={[0.05, 0.05, 0.05]} /><meshStandardMaterial color="#000" /></mesh>
                      <mesh position={[0, 0.3, -0.28]}><boxGeometry args={[0.2, 0.2, 0.1]} /><meshStandardMaterial color="#FFF" /></mesh> {/* Tail */}
                      <mesh position={[0.15, 0.05, 0]}><boxGeometry args={[0.1, 0.2, 0.1]} /><meshStandardMaterial color="#FFA000" /></mesh>
                      <mesh position={[-0.15, 0.05, 0]}><boxGeometry args={[0.1, 0.2, 0.1]} /><meshStandardMaterial color="#FFA000" /></mesh>
                      <mesh position={[0, 0.15, 0.28]}><boxGeometry args={[0.1, 0.1, 0.1]} /><meshStandardMaterial color="#FF3333" /></mesh> {/* Wattle */}
                  </group>
              );
          case 'pig':
              return (
                  <group>
                       <mesh position={[0, 0.3, 0]} castShadow><boxGeometry args={[0.5, 0.4, 0.6]} /><meshStandardMaterial color="#F48FB1" /></mesh>
                       <mesh position={[0, 0.25, 0.35]} castShadow><boxGeometry args={[0.2, 0.15, 0.1]} /><meshStandardMaterial color="#EC407A" /></mesh> {/* Nose */}
                       <mesh position={[0.15, 0.35, 0.31]}><boxGeometry args={[0.05, 0.05, 0.05]} /><meshStandardMaterial color="#000" /></mesh>
                       <mesh position={[-0.15, 0.35, 0.31]}><boxGeometry args={[0.05, 0.05, 0.05]} /><meshStandardMaterial color="#000" /></mesh>
                       <mesh position={[0.15, 0.05, 0.2]}><boxGeometry args={[0.12, 0.2, 0.12]} /><meshStandardMaterial color="#F06292" /></mesh>
                       <mesh position={[-0.15, 0.05, 0.2]}><boxGeometry args={[0.12, 0.2, 0.12]} /><meshStandardMaterial color="#F06292" /></mesh>
                       <mesh position={[0.15, 0.05, -0.2]}><boxGeometry args={[0.12, 0.2, 0.12]} /><meshStandardMaterial color="#F06292" /></mesh>
                       <mesh position={[-0.15, 0.05, -0.2]}><boxGeometry args={[0.12, 0.2, 0.12]} /><meshStandardMaterial color="#F06292" /></mesh>
                       <mesh position={[0.2, 0.45, 0.2]}><boxGeometry args={[0.1, 0.1, 0.05]} /><meshStandardMaterial color="#F48FB1" /></mesh>
                       <mesh position={[-0.2, 0.45, 0.2]}><boxGeometry args={[0.1, 0.1, 0.05]} /><meshStandardMaterial color="#F48FB1" /></mesh>
                  </group>
              );
          case 'penguin':
               return (
                   <group>
                        <mesh position={[0, 0.35, 0]} castShadow><boxGeometry args={[0.45, 0.6, 0.4]} /><meshStandardMaterial color="#212121" /></mesh>
                        <mesh position={[0, 0.35, 0.21]}><planeGeometry args={[0.3, 0.5]} /><meshStandardMaterial color="#FFF" /></mesh>
                        <mesh position={[0, 0.5, 0.25]}><boxGeometry args={[0.15, 0.1, 0.1]} /><meshStandardMaterial color="#FFB300" /></mesh> {/* Beak */}
                        <mesh position={[0.1, 0.55, 0.2]}><boxGeometry args={[0.05, 0.05, 0.05]} /><meshStandardMaterial color="#000" /></mesh>
                        <mesh position={[-0.1, 0.55, 0.2]}><boxGeometry args={[0.05, 0.05, 0.05]} /><meshStandardMaterial color="#000" /></mesh>
                        <mesh position={[0.15, 0.05, 0]}><boxGeometry args={[0.12, 0.1, 0.2]} /><meshStandardMaterial color="#FFB300" /></mesh>
                        <mesh position={[-0.15, 0.05, 0]}><boxGeometry args={[0.12, 0.1, 0.2]} /><meshStandardMaterial color="#FFB300" /></mesh>
                   </group>
               );
           case 'bear':
               return (
                   <group>
                       <mesh position={[0, 0.3, 0]} castShadow><boxGeometry args={[0.6, 0.5, 0.7]} /><meshStandardMaterial color="#F5F5F5" /></mesh>
                       <mesh position={[0.25, 0.55, 0.2]}><boxGeometry args={[0.1, 0.1, 0.1]} /><meshStandardMaterial color="#F5F5F5" /></mesh>
                       <mesh position={[-0.25, 0.55, 0.2]}><boxGeometry args={[0.1, 0.1, 0.1]} /><meshStandardMaterial color="#F5F5F5" /></mesh>
                       <mesh position={[0, 0.3, 0.4]}><boxGeometry args={[0.2, 0.15, 0.1]} /><meshStandardMaterial color="#E0E0E0" /></mesh>
                       <mesh position={[0, 0.35, 0.46]}><boxGeometry args={[0.08, 0.05, 0.05]} /><meshStandardMaterial color="#212121" /></mesh>
                       <mesh position={[0.15, 0.05, 0.2]}><boxGeometry args={[0.15, 0.2, 0.15]} /><meshStandardMaterial color="#F5F5F5" /></mesh>
                       <mesh position={[-0.15, 0.05, 0.2]}><boxGeometry args={[0.15, 0.2, 0.15]} /><meshStandardMaterial color="#F5F5F5" /></mesh>
                       <mesh position={[0.15, 0.05, -0.2]}><boxGeometry args={[0.15, 0.2, 0.15]} /><meshStandardMaterial color="#F5F5F5" /></mesh>
                       <mesh position={[-0.15, 0.05, -0.2]}><boxGeometry args={[0.15, 0.2, 0.15]} /><meshStandardMaterial color="#F5F5F5" /></mesh>
                   </group>
               );
            case 'robot':
                return (
                    <group>
                        <mesh position={[0, 0.4, 0]} castShadow><boxGeometry args={[0.4, 0.5, 0.3]} /><meshStandardMaterial color="#78909C" /></mesh>
                        <mesh position={[0, 0.5, 0.16]}><boxGeometry args={[0.3, 0.1, 0.05]} /><meshStandardMaterial color="#111" /></mesh>
                        <mesh position={[0.08, 0.5, 0.17]}><boxGeometry args={[0.08, 0.08, 0.05]} /><meshStandardMaterial color="#00E676" emissive="#00E676" emissiveIntensity={2} /></mesh>
                        <mesh position={[-0.08, 0.5, 0.17]}><boxGeometry args={[0.08, 0.08, 0.05]} /><meshStandardMaterial color="#00E676" emissive="#00E676" emissiveIntensity={2} /></mesh>
                        <mesh position={[0, 0.7, 0]}><boxGeometry args={[0.02, 0.3, 0.02]} /><meshStandardMaterial color="#CFD8DC" /></mesh>
                        <mesh position={[0, 0.85, 0]}><sphereGeometry args={[0.05]} /><meshStandardMaterial color="#F44336" emissive="#F44336" /></mesh>
                        <mesh position={[0.15, 0.1, 0]}><boxGeometry args={[0.1, 0.2, 0.1]} /><meshStandardMaterial color="#546E7A" /></mesh>
                        <mesh position={[-0.15, 0.1, 0]}><boxGeometry args={[0.1, 0.2, 0.1]} /><meshStandardMaterial color="#546E7A" /></mesh>
                    </group>
                )
            case 'zombie':
                return (
                    <group>
                        <mesh position={[0, 0.45, 0]} castShadow><boxGeometry args={[0.4, 0.4, 0.3]} /><meshStandardMaterial color="#7CB342" /></mesh>
                        <mesh position={[0, 0.2, 0]} castShadow><boxGeometry args={[0.4, 0.3, 0.3]} /><meshStandardMaterial color="#1E88E5" /></mesh>
                        <mesh position={[0.3, 0.4, 0.2]} rotation={[0.4, 0, 0]}><boxGeometry args={[0.1, 0.3, 0.1]} /><meshStandardMaterial color="#7CB342" /></mesh>
                        <mesh position={[-0.3, 0.4, 0.2]} rotation={[0.4, 0, 0]}><boxGeometry args={[0.1, 0.3, 0.1]} /><meshStandardMaterial color="#7CB342" /></mesh>
                        <mesh position={[0.15, 0.05, 0]}><boxGeometry args={[0.12, 0.3, 0.12]} /><meshStandardMaterial color="#37474F" /></mesh>
                        <mesh position={[-0.15, 0.05, 0]}><boxGeometry args={[0.12, 0.3, 0.12]} /><meshStandardMaterial color="#37474F" /></mesh>
                        <mesh position={[0.1, 0.5, 0.16]}><boxGeometry args={[0.05, 0.05, 0.01]} /><meshStandardMaterial color="#000" /></mesh>
                        <mesh position={[-0.1, 0.5, 0.16]}><boxGeometry args={[0.08, 0.08, 0.01]} /><meshStandardMaterial color="#000" /></mesh>
                    </group>
                )
            case 'vampire':
                return (
                    <group>
                        <mesh position={[0, 0.4, 0]} castShadow><boxGeometry args={[0.4, 0.6, 0.3]} /><meshStandardMaterial color="#212121" /></mesh>
                        <mesh position={[0, 0.5, 0.16]}><planeGeometry args={[0.15, 0.2]} /><meshStandardMaterial color="#FFF" /></mesh>
                        <mesh position={[0, 0.5, -0.16]}><boxGeometry args={[0.5, 0.6, 0.05]} /><meshStandardMaterial color="#B71C1C" /></mesh> {/* Cape */}
                        <mesh position={[0, 0.75, -0.16]}><boxGeometry args={[0.5, 0.2, 0.05]} /><meshStandardMaterial color="#B71C1C" /></mesh> {/* Collar */}
                        <mesh position={[0.1, 0.55, 0.17]}><boxGeometry args={[0.04, 0.04, 0.01]} /><meshStandardMaterial color="#D50000" /></mesh>
                        <mesh position={[-0.1, 0.55, 0.17]}><boxGeometry args={[0.04, 0.04, 0.01]} /><meshStandardMaterial color="#D50000" /></mesh>
                    </group>
                )
            case 'golden':
                return (
                     <group>
                      <mesh position={[0, 0.3, 0]} castShadow><boxGeometry args={[0.5, 0.5, 0.5]} /><meshStandardMaterial color="#FFD700" metalness={0.8} roughness={0.1} /></mesh>
                      <mesh position={[0, 0.55, 0.2]} castShadow><boxGeometry args={[0.1, 0.1, 0.2]} /><meshStandardMaterial color="#FFD700" metalness={0.8} roughness={0.1} /></mesh> 
                      <mesh position={[0, 0.3, 0.3]} castShadow><boxGeometry args={[0.15, 0.15, 0.1]} /><meshStandardMaterial color="#FFA000" /></mesh> 
                      <mesh position={[0, 0.6, 0]}><cylinderGeometry args={[0.2, 0.2, 0.1, 8]} /><meshStandardMaterial color="#E91E63" /></mesh> {/* Crown Base */}
                      <mesh position={[0, 0.68, 0]}><cylinderGeometry args={[0.25, 0.15, 0.1, 5]} /><meshStandardMaterial color="#FFD700" metalness={1} /></mesh> {/* Crown */}
                      <mesh position={[0.15, 0.05, 0]}><boxGeometry args={[0.1, 0.2, 0.1]} /><meshStandardMaterial color="#FFD700" metalness={0.8} /></mesh>
                      <mesh position={[-0.15, 0.05, 0]}><boxGeometry args={[0.1, 0.2, 0.1]} /><meshStandardMaterial color="#FFD700" metalness={0.8} /></mesh>
                  </group>
                )
          default: return null;
      }
  }

  return (
    <group position={visualPosition}>
        <group ref={groupRef}>
            {renderModel()}
        </group>
        <PlayerParticles isMoving={isMoving} position={new THREE.Vector3(0,0,0)} skinId={skinId} />
    </group>
  );
};

const Car: React.FC<{ laneData: ExtendedLaneData, offset: number, playerPos: THREE.Vector3, onCollision: (reason: DeathReason) => void }> = ({ laneData, offset, playerPos, onCollision }) => {
  const groupRef = useRef<THREE.Group>(null);
  const speed = laneData.carSpeed || 0;
  const color = useMemo(() => ['#E74C3C', '#8E44AD', '#3498DB', '#F1C40F', '#FFF'][Math.floor(Math.random()*5)], []);
  const range = 50;

  useFrame((state) => {
    if (!groupRef.current) return;
    const time = state.clock.elapsedTime;
    const rawPos = (time * speed) + offset;
    let x = ((rawPos % range) + range) % range;
    x -= range / 2;
    groupRef.current.position.set(x, 0.25, 0);
    // Simple Box Collision
    if (Math.abs(laneData.index - Math.round(playerPos.z)) < 0.4) {
       const dist = Math.abs(x - playerPos.x);
       if (dist < 1.0) onCollision('FLATTENED');
    }
  });

  const isFacingRight = speed > 0;
  return (
    <group ref={groupRef}>
      <group rotation={[0, isFacingRight ? 0 : Math.PI, 0]}>
        <mesh position={[0, 0.35, 0]} castShadow><boxGeometry args={[1.6, 0.5, 0.8]} /><meshStandardMaterial color={color} /></mesh>
        <mesh position={[-0.1, 0.7, 0]} castShadow><boxGeometry args={[1.0, 0.4, 0.72]} /><meshStandardMaterial color="#333" /></mesh>
        <mesh position={[-0.1, 0.7, 0]}><boxGeometry args={[1.02, 0.3, 0.74]} /><meshStandardMaterial color="#87CEEB" roughness={0.2} /></mesh>
        <mesh position={[0.5, 0.1, 0.35]}><boxGeometry args={[0.3, 0.3, 0.1]} /><meshStandardMaterial color="#111" /></mesh>
        <mesh position={[0.5, 0.1, -0.35]}><boxGeometry args={[0.3, 0.3, 0.1]} /><meshStandardMaterial color="#111" /></mesh>
        <mesh position={[-0.5, 0.1, 0.35]}><boxGeometry args={[0.3, 0.3, 0.1]} /><meshStandardMaterial color="#111" /></mesh>
        <mesh position={[-0.5, 0.1, -0.35]}><boxGeometry args={[0.3, 0.3, 0.1]} /><meshStandardMaterial color="#111" /></mesh>
      </group>
    </group>
  );
};

const Train: React.FC<{ laneData: ExtendedLaneData, playerPos: THREE.Vector3, onCollision: (reason: DeathReason) => void, triggerShake: (amt: number) => void }> = ({ laneData, playerPos, onCollision, triggerShake }) => {
    const groupRef = useRef<THREE.Group>(null);
    const lightRef = useRef<THREE.Group>(null);
    const [warning, setWarning] = useState(false);
    const cycleRef = useRef(Math.random() * 5); 

    useFrame((state) => {
        const time = state.clock.elapsedTime + cycleRef.current;
        const cycle = time % 10; 
        if (cycle > 6 && cycle < 9) {
            setWarning(true);
        } else if (cycle > 9 && cycle < 9.6) {
            setWarning(false);
            const trainProgress = (cycle - 9) / 0.6; 
            const startX = -50; const endX = 50;
            const x = THREE.MathUtils.lerp(startX, endX, trainProgress);
            if (groupRef.current) groupRef.current.position.set(x, 0.5, 0);
            
            if (trainProgress > 0.4 && trainProgress < 0.6) {
                if (Math.abs(laneData.index - playerPos.z) < 8) triggerShake(0.1);
            }

            if (Math.abs(laneData.index - Math.round(playerPos.z)) < 0.4) {
                if (Math.abs(x - playerPos.x) < 4.5) { onCollision('SMASHED'); }
            }
            if (trainProgress < 0.1 && Math.random() < 0.1) playSound('train');
        } else {
            setWarning(false);
            if (groupRef.current) groupRef.current.position.set(-60, 0, 0);
        }
        if (lightRef.current) lightRef.current.visible = warning && (Math.floor(time * 8) % 2 === 0);
    });

    return (
        <group>
            {/* Rails - Slightly raised to 0.03 to avoid Z-fight with road/grass */}
            <mesh position={[0, 0.03, 0.2]} receiveShadow><boxGeometry args={[PLAYABLE_WIDTH + 20, 0.1, 0.1]} /><meshStandardMaterial color={STATIC_COLORS.RAIL_METAL} /></mesh>
            <mesh position={[0, 0.03, -0.2]} receiveShadow><boxGeometry args={[PLAYABLE_WIDTH + 20, 0.1, 0.1]} /><meshStandardMaterial color={STATIC_COLORS.RAIL_METAL} /></mesh>
            
            {Array.from({length: 25}).map((_, i) => (
                <mesh key={i} position={[i * 1.5 - 18, 0.02, 0]} receiveShadow><boxGeometry args={[0.3, 0.05, 0.8]} /><meshStandardMaterial color="#3E2723" /></mesh>
            ))}

            <group position={[-5, 0, -1]}>
                 <mesh position={[0, 1, 0]}><boxGeometry args={[0.2, 2, 0.2]} /><meshStandardMaterial color="#333" /></mesh>
                 <group ref={lightRef}><mesh position={[0, 2, 0.1]}><sphereGeometry args={[0.2]} /><meshStandardMaterial color="red" emissive="red" emissiveIntensity={3} /></mesh></group>
            </group>

            <group ref={groupRef} position={[-50, 0, 0]}>
                <mesh position={[0, 0.5, 0]} castShadow><boxGeometry args={[8, 1.2, 0.9]} /><meshStandardMaterial color={STATIC_COLORS.TRAIN_BODY} /></mesh>
                <mesh position={[4, 0.2, 0]}><boxGeometry args={[0.1, 0.4, 0.6]} /><meshStandardMaterial color="#FFFF00" emissive="#FFFF00" /></mesh>
                <mesh position={[0, 0.6, 0.46]}><boxGeometry args={[7, 0.4, 0.1]} /><meshStandardMaterial color="#222" /></mesh>
            </group>
        </group>
    )
}

const Log: React.FC<{ laneData: ExtendedLaneData, offset: number }> = ({ laneData, offset }) => {
    const groupRef = useRef<THREE.Group>(null);
    const speed = laneData.logSpeed || 0;
    const range = 50; // Match car range for simpler logic
    
    useFrame((state) => {
        if (!groupRef.current) return;
        const time = state.clock.elapsedTime;
        const rawPos = (time * speed) + offset;
        let x = ((rawPos % range) + range) % range;
        x -= range / 2;
        groupRef.current.position.set(x, -0.15, 0); 
    });

    return (
        <group ref={groupRef}>
            {/* Wider Log */}
            <mesh rotation={[0,0,Math.PI/2]} castShadow receiveShadow>
                <cylinderGeometry args={[0.35, 0.35, 3.5, 8]} />
                <meshStandardMaterial color={STATIC_COLORS.LOG} />
            </mesh>
            {/* End Caps detail */}
            <mesh position={[1.7, 0, 0]} rotation={[0,0,Math.PI/2]}>
                <cylinderGeometry args={[0.38, 0.38, 0.1, 8]} />
                <meshStandardMaterial color={STATIC_COLORS.LOG_DARK} />
            </mesh>
             <mesh position={[-1.7, 0, 0]} rotation={[0,0,-Math.PI/2]}>
                <cylinderGeometry args={[0.38, 0.38, 0.1, 8]} />
                <meshStandardMaterial color={STATIC_COLORS.LOG_DARK} />
            </mesh>
        </group>
    )
}

const Scenery: React.FC<{ position: [number, number, number], biome: any }> = React.memo(({ position, biome }) => {
    return (
        <group position={position}>
             <mesh position={[0, 2, 0]} castShadow receiveShadow>
                <coneGeometry args={[3, 8, 4]} />
                <meshStandardMaterial color="#78909C" flatShading />
            </mesh>
            <mesh position={[0, 5.5, 0]} castShadow>
                <coneGeometry args={[1.5, 3, 4]} />
                <meshStandardMaterial color="#ECEFF1" flatShading />
            </mesh>
        </group>
    )
})

const Tree: React.FC<{ position: [number, number, number], variant: number, biome: any }> = React.memo(({ position, variant, biome }) => {
    // variant 5 = rock (no tree)
    if (variant === 5) return <mesh position={[position[0], 0.3, position[2]]} castShadow receiveShadow><dodecahedronGeometry args={[0.4, 0]} /><meshStandardMaterial color={STATIC_COLORS.ROCK} flatShading /></mesh>

    // Different tree heights: 0=tallest, 1=tall, 2=medium, 3=short, 4=shortest
    const heights = [2.0, 1.7, 1.4, 1.1, 0.8];
    const height = heights[variant] || 1.0;
    const trunkHeight = 0.8;
    const foliageWidth = 0.8;

    // Color variation based on height
    const color = variant <= 1 ? biome.colors.TREE : biome.colors.TREE_DARK;

    return (
        <group position={position}>
            <mesh position={[0, trunkHeight/2, 0]} castShadow><boxGeometry args={[0.3, trunkHeight, 0.3]} /><meshStandardMaterial color="#5D4037" /></mesh>
            <mesh position={[0, trunkHeight + height/2, 0]} castShadow><boxGeometry args={[foliageWidth, height, foliageWidth]} /><meshStandardMaterial color={color} /></mesh>
        </group>
    )
});

const Lane: React.FC<{ data: ExtendedLaneData, playerPos: THREE.Vector3, onCollision: (reason: DeathReason) => void, collectedCoins: Set<string>, triggerShake: (n: number) => void }> = ({ data, playerPos, onCollision, collectedCoins, triggerShake }) => {
  const biome = BIOMES[data.biomeIndex];
  const cars = useMemo(() => {
    if (data.type !== LaneType.ROAD) return [];
    const items = [];
    const count = data.carCount || 2;
    for (let i=0; i<count; i++) items.push(i * (50/count));
    return items;
  }, [data]);
  
  const groundColor = useMemo(() => {
      switch(data.type) {
          case LaneType.ROAD: return biome.colors.ROAD;
          case LaneType.RIVER: return biome.colors.WATER;
          case LaneType.RAIL: return biome.colors.GRASS;
          default: return biome.colors.GRASS;
      }
  }, [data.type, biome]);

  const sideColor = useMemo(() => {
     switch(data.type) {
        case LaneType.RIVER: return biome.colors.WATER_DEEP;
        default: return '#5D4037';
     }
  }, [data.type, biome]);

  return (
    <group position={[0, 0, data.index]}>
      {/* Thick Base - Moved down to -6.15 to prevent Z-fight with surface */}
      <mesh receiveShadow position={[0, -6.15, 0]}><boxGeometry args={[50, 12, 1]} /><meshStandardMaterial color={sideColor} /></mesh>
      
      {/* Surface Plane */}
      <mesh receiveShadow position={[0, -0.1, 0]} rotation={[-Math.PI/2, 0, 0]}><planeGeometry args={[50, 1]} /><meshStandardMaterial color={groundColor} /></mesh>
      
      {data.type === LaneType.ROAD && (
          <>
            {/* Markings slightly elevated to 0.02 */}
            <mesh position={[0, 0.02, 0.45]}><boxGeometry args={[50, 0.1, 0.1]} /><meshStandardMaterial color="#999" /></mesh>
            <mesh position={[0, 0.02, -0.45]}><boxGeometry args={[50, 0.1, 0.1]} /><meshStandardMaterial color="#999" /></mesh>
            {Array.from({length: 12}).map((_, i) => (
                <mesh key={i} position={[i * 4 - 22, 0.02, 0]} rotation={[-Math.PI/2, 0, 0]}><planeGeometry args={[2, 0.15]} /><meshStandardMaterial color="#FFF" /></mesh>
            ))}
          </>
      )}

      {data.type === LaneType.RIVER && <mesh position={[0, -0.18, 0]} rotation={[-Math.PI/2, 0, 0]}><planeGeometry args={[50, 1]} /><meshStandardMaterial color={biome.colors.WATER_DEEP} /></mesh>}
      
      {data.sceneryLeft.map((x, i) => <Scenery key={`sc-l-${data.id}-${i}`} position={[x, 0, 0]} biome={biome} />)}
      {data.sceneryRight.map((x, i) => <Scenery key={`sc-r-${data.id}-${i}`} position={[x, 0, 0]} biome={biome} />)}
      
      {Array.from(data.obstacles).map((x) => <Tree key={`obs-${data.id}-${x}`} position={[x, 0, 0]} variant={Math.abs(x) % 6} biome={biome} />)}
      {Array.from(data.coins).map(x => <Coin key={`coin-${data.id}-${x}`} position={[x, 0, 0]} collected={collectedCoins.has(`${data.index},${x}`)} />)}
      
      {cars.map((offset, i) => <Car key={`car-${data.id}-${i}`} laneData={data} offset={offset} playerPos={playerPos} onCollision={onCollision} />)}
      {data.type === LaneType.RAIL && <Train laneData={data} playerPos={playerPos} onCollision={onCollision} triggerShake={triggerShake} />}
      {data.logOffsets && data.logOffsets.map((offset, i) => <Log key={`log-${data.id}-${i}`} laneData={data} offset={offset} />)}
    </group>
  );
};

// --- MAIN GAME CONTROLLER ---

interface GameControllerProps {
    status: GameStatus;
    setStatus: (s: GameStatus) => void;
    setScore: React.Dispatch<React.SetStateAction<number>>;
    setCoins: React.Dispatch<React.SetStateAction<number>>;
    currentSkin: string;
    highScore: number;
    onDeath: (reason: DeathReason) => void;
}

interface GenState {
    lastType: LaneType;
    consecutive: number;
    forceGrass: boolean;
}

export const VoxelGame: React.FC<GameControllerProps> = ({ status, setStatus, setScore, setCoins, currentSkin, highScore, onDeath }) => {
  const [lanes, setLanes] = useState<ExtendedLaneData[]>([]);
  const [collectedCoins, setCollectedCoins] = useState<Set<string>>(new Set());
  
  const playerPos = useRef(new THREE.Vector3(0, 0, 0));
  const targetPos = useRef(new THREE.Vector3(0, 0, 0));
  const isMoving = useRef(false);
  const moveStartTime = useRef(0);
  const startPos = useRef(new THREE.Vector3(0, 0, 0));
  const direction = useRef(0);
  const cameraShake = useRef(0);
  const bonkingBack = useRef(false); // Track if player is bonking back from obstacle
  
  // Track generation constraints
  const genState = useRef<GenState>({ lastType: LaneType.GRASS, consecutive: 0, forceGrass: false });

  const [visualState, setVisualState] = useState({ moving: false, dir: 0, jump: 0 });
  const [renderPos, setRenderPos] = useState(new THREE.Vector3(0,0,0));
  const [currentBiome, setCurrentBiome] = useState('PLAINS');

  const generateLane = useCallback((index: number, state: GenState): ExtendedLaneData => {
      const section = Math.floor(index / 50);
      const len = BIOMES.length;
      const biomeIndex = ((section % len) + len) % len;

      const sceneryLeft = [];
      const sceneryRight = [];
      // Push scenery further out due to wider playable width
      if (Math.random() < 0.3) sceneryLeft.push(-22 - Math.random() * 5); 
      if (Math.random() < 0.3) sceneryRight.push(22 + Math.random() * 5);

      if (index < 5) {
          state.lastType = LaneType.GRASS;
          state.consecutive = 0;
          return {
              id: index, index, type: LaneType.GRASS, speed: 0, density: 0, direction: 1,
              obstacles: new Set(), coins: new Set(), sceneryLeft, sceneryRight, biomeIndex
          };
      }

      let type = LaneType.GRASS;
      let carSpeed = 0; let logSpeed = 0;
      let obstacles = new Set<number>(); let coins = new Set<number>();
      let logOffsets: number[] = [];

      const r = Math.random();
      
      if (state.forceGrass) {
          type = LaneType.GRASS;
          state.forceGrass = false;
      } else {
          if (r < 0.35) type = LaneType.ROAD;
          else if (r < 0.55) type = LaneType.RIVER;
          else if (r < 0.65) type = LaneType.RAIL;
          else type = LaneType.GRASS;

          if (type === LaneType.ROAD) {
              if (state.lastType === LaneType.ROAD && state.consecutive >= 3) {
                  type = LaneType.GRASS;
              }
          } else if (type === LaneType.RIVER) {
              if (state.lastType !== LaneType.GRASS && state.lastType !== LaneType.RIVER) {
                  type = LaneType.GRASS;
              } else if (state.lastType === LaneType.RIVER && state.consecutive >= 2) {
                  type = LaneType.GRASS;
              }
          } else if (type === LaneType.RAIL) {
              if (state.lastType === LaneType.RAIL) {
                  type = LaneType.GRASS;
              }
              if (state.lastType !== LaneType.GRASS) {
                  state.forceGrass = true;
              }
          }
      }

      if (state.lastType === LaneType.RIVER && (type === LaneType.ROAD || type === LaneType.RAIL)) {
          type = LaneType.GRASS;
      }

      if (type === state.lastType) {
          state.consecutive++;
      } else {
          state.lastType = type;
          state.consecutive = 1;
      }

      if (type === LaneType.ROAD) {
          const speedFactor = Math.random() * 5 + 4;
          carSpeed = (index % 2 === 0 ? 1 : -1) * speedFactor;
      } else if (type === LaneType.RIVER) {
          const speedFactor = Math.random() * 3 + 2;
          logSpeed = (index % 2 !== 0 ? 1 : -1) * speedFactor;
          const logCount = Math.floor(Math.random() * 2) + 2;
          for(let i=0; i<logCount; i++) logOffsets.push(i * (50/logCount) + Math.random() * 5);
      } else if (type === LaneType.GRASS) {
          // Increase obstacle count for wider map
          const numObs = Math.floor(Math.random() * (PLAYABLE_WIDTH / 2.5));
          for(let i=0; i<numObs; i++) {
              let x = Math.floor(Math.random() * (PLAYABLE_WIDTH + 1)) - Math.floor(PLAYABLE_WIDTH/2);
              if (index === 0 && x === 0) continue;
              obstacles.add(x);
          }
          // Higher Coin Density
          if (Math.random() < 0.4) {
             const coinCount = Math.floor(Math.random() * 3) + 1; // 1 to 3 coins
             for(let k=0; k<coinCount; k++) {
                 let x = Math.floor(Math.random() * (PLAYABLE_WIDTH + 1)) - Math.floor(PLAYABLE_WIDTH/2);
                 if (!obstacles.has(x)) coins.add(x);
             }
          }
      }

      return {
          id: index, index, type, speed: 0, density: 0, direction: 1,
          carSpeed, logSpeed, carCount: Math.floor(Math.random() * 2) + 2, logCount: logOffsets.length, logOffsets,
          obstacles, coins, sceneryLeft, sceneryRight, biomeIndex
      };
  }, []);

  const initGame = useCallback(() => {
    setScore(0);
    setCollectedCoins(new Set());
    genState.current = { lastType: LaneType.GRASS, consecutive: 0, forceGrass: false };
    const initLanes = [];
    for(let i=-5; i<25; i++) initLanes.push(generateLane(i, genState.current));
    setLanes(initLanes);
    playerPos.current.set(0, 0, 0);
    targetPos.current.set(0, 0, 0);
    setRenderPos(new THREE.Vector3(0,0,0));
    direction.current = 0;
    setVisualState({ moving: false, dir: 0, jump: 0 });
    cameraShake.current = 0;
  }, [generateLane, setScore]);

  const lastStatusRef = useRef<GameStatus>(status);

  useEffect(() => {
    const statusChanged = lastStatusRef.current !== status;
    lastStatusRef.current = status;

    if (status === GameStatus.PLAYING && statusChanged) {
        initGame();
    } else if (status === GameStatus.MENU) {
        initGame();
    }
  }, [status, initGame]);

  const canMoveTo = useCallback((x: number, z: number) => {
      if (x < -Math.floor(PLAYABLE_WIDTH/2) || x > Math.floor(PLAYABLE_WIDTH/2)) return false;
      const lane = lanes.find(l => l.index === z);
      if (!lane) return false;
      if (lane.obstacles.has(x)) return false;
      return true;
  }, [lanes]);

  useEffect(() => {
      const handleKeyDown = (e: KeyboardEvent) => {
          if (status !== GameStatus.PLAYING) return;
          if (isMoving.current) return;
          let dx = 0; let dz = 0; let dir = direction.current;
          if (e.key === 'ArrowUp' || e.key === 'w') { dx=0; dz=1; dir=0; }
          else if (e.key === 'ArrowDown' || e.key === 's') { dx=0; dz=-1; dir=2; }
          else if (e.key === 'ArrowLeft' || e.key === 'a') { dx=1; dz=0; dir=1; }
          else if (e.key === 'ArrowRight' || e.key === 'd') { dx=-1; dz=0; dir=3; }
          else return;
          const newX = Math.round(playerPos.current.x + dx);
          const newZ = Math.round(playerPos.current.z + dz);

          // Check if hitting obstacle
          const lane = lanes.find(l => l.index === newZ);
          const hitObstacle = lane && lane.obstacles.has(newX);

          if (canMoveTo(newX, newZ)) {
              playSound('jump');
              isMoving.current = true;
              bonkingBack.current = false;
              moveStartTime.current = performance.now();
              startPos.current.copy(playerPos.current);
              targetPos.current.set(newX, 0, newZ);
              direction.current = dir;
              setVisualState(prev => ({ ...prev, moving: true, dir }));
          } else if (hitObstacle) {
              // Hit obstacle: jump forward a bit then bounce back
              // Calculate pitch based on obstacle variant (tree height)
              const variant = Math.abs(newX) % 6;
              // Map variant to pitch: 0=tallest(2.0), 1=tall(1.7), 2=medium(1.4), 3=short(1.1), 4=shortest(0.8), 5=rock
              const pitchMap = [2.0, 1.7, 1.4, 1.1, 0.9, 0.8]; // Higher trees = higher pitch
              const pitch = pitchMap[variant] || 1.0;
              playSound('bonk', pitch);
              isMoving.current = true;
              bonkingBack.current = true;
              moveStartTime.current = performance.now();
              startPos.current.copy(playerPos.current);
              // Target is partway to the obstacle (30% of the way)
              const bonkX = playerPos.current.x + dx * 0.3;
              const bonkZ = playerPos.current.z + dz * 0.3;
              targetPos.current.set(bonkX, 0, bonkZ);
              direction.current = dir;
              setVisualState(prev => ({ ...prev, moving: true, dir }));
          }
      };
      window.addEventListener('keydown', handleKeyDown);
      return () => window.removeEventListener('keydown', handleKeyDown);
  }, [status, canMoveTo, lanes]);

  const handleDeath = useCallback((reason: DeathReason) => {
      if (status === GameStatus.PLAYING) {
          // Play specific death sound based on death reason
          switch(reason) {
              case 'FLATTENED':
                  playSound('flattened');
                  break;
              case 'DROWNED':
                  playSound('drowned');
                  break;
              case 'SMASHED':
                  playSound('smashed');
                  break;
              case 'LOST':
                  playSound('lost');
                  break;
              default:
                  playSound('die');
          }
          onDeath(reason);
          cameraShake.current = 0.5;
          setTimeout(() => setStatus(GameStatus.GAME_OVER), 500);
      }
  }, [status, setStatus, onDeath]);

  useFrame((state, delta) => {
      if (status !== GameStatus.PLAYING && status !== GameStatus.GAME_OVER) return;
      
      const time = state.clock.elapsedTime;
      const currentZ = Math.round(playerPos.current.z);
      const lane = lanes.find(l => l.index === currentZ);

      if (lane) setCurrentBiome(BIOMES[lane.biomeIndex].name);

      if (isMoving.current) {
          const now = performance.now();
          const elapsed = (now - moveStartTime.current) / 1000;
          const moveDuration = bonkingBack.current ? MOVEMENT_SPEED * 0.6 : MOVEMENT_SPEED; // Bonk is faster

          if (elapsed < moveDuration) {
              const t = elapsed / moveDuration;
              if (bonkingBack.current) {
                  // For bonk: go forward then back
                  if (t < 0.5) {
                      // First half: move forward
                      const forwardT = t * 2;
                      playerPos.current.lerpVectors(startPos.current, targetPos.current, forwardT);
                      setVisualState(prev => ({ ...prev, jump: forwardT }));
                  } else {
                      // Second half: bounce back
                      const backT = (t - 0.5) * 2;
                      playerPos.current.lerpVectors(targetPos.current, startPos.current, backT);
                      setVisualState(prev => ({ ...prev, jump: 1 - backT }));
                  }
              } else {
                  // Normal jump
                  playerPos.current.lerpVectors(startPos.current, targetPos.current, t);
                  setVisualState(prev => ({ ...prev, jump: t }));
              }
          } else {
              // Movement complete
              if (bonkingBack.current) {
                  // Bonk complete: return to start position
                  playerPos.current.copy(startPos.current);
                  bonkingBack.current = false;
              } else {
                  playerPos.current.copy(targetPos.current);
              }
              isMoving.current = false;
              setVisualState(prev => ({ ...prev, moving: false, jump: 0 }));
              const landedLane = lanes.find(l => l.index === Math.round(playerPos.current.z));
              
              if (landedLane) {
                  const coinKey = `${landedLane.index},${Math.round(playerPos.current.x)}`;
                  if (landedLane.coins.has(Math.round(playerPos.current.x))) {
                       if (!collectedCoins.has(coinKey)) {
                           playSound('coin');
                           setCollectedCoins(prev => new Set(prev).add(coinKey));
                           setCoins(c => c + 1);
                       }
                  }
                  if (playerPos.current.z > 0) setScore(s => Math.max(s, Math.round(playerPos.current.z)));
              }
              
              if (lanes.length > 0) {
                  const lastLane = lanes[lanes.length-1];
                  const lastIndex = lastLane ? lastLane.index : 0;
                  
                  if (playerPos.current.z > lastIndex - 20) {
                      const newLanes: ExtendedLaneData[] = [];
                      for(let i=1; i<=10; i++) newLanes.push(generateLane(lastIndex + i, genState.current));
                      setLanes(prev => {
                          const keep = prev.filter(l => l.index > playerPos.current.z - 15); 
                          return [...keep, ...newLanes];
                      });
                  }
              }
          }
      }

      if (!isMoving.current && lane?.type === LaneType.RIVER && status === GameStatus.PLAYING) {
          const range = 50;
          const speed = lane.logSpeed || 0;
          let safe = false;
          let logVelocity = 0;
          
          if (lane.logOffsets) {
              for(const offset of lane.logOffsets) {
                  const rawPos = (time * speed) + offset;
                  let logX = ((rawPos % range) + range) % range;
                  logX -= range / 2;
                  if (Math.abs(logX - playerPos.current.x) < 1.8) {
                      safe = true;
                      logVelocity = speed;
                      break;
                  }
              }
          }
          if (safe) {
              playerPos.current.x += logVelocity * delta;
          } else {
              handleDeath('DROWNED');
          }
      }

      if (Math.abs(playerPos.current.x) > (PLAYABLE_WIDTH/2 + 1.0)) {
          handleDeath('LOST');
      }

      const shakeX = (Math.random() - 0.5) * cameraShake.current;
      const shakeY = (Math.random() - 0.5) * cameraShake.current;
      const shakeZ = (Math.random() - 0.5) * cameraShake.current;
      if (cameraShake.current > 0) cameraShake.current = THREE.MathUtils.lerp(cameraShake.current, 0, 0.1);

      const camOffset = new THREE.Vector3(-10, 10, -10);
      const idealPos = new THREE.Vector3(playerPos.current.x + camOffset.x, 12, playerPos.current.z + camOffset.z);
      state.camera.position.lerp(idealPos, 0.1);
      state.camera.position.add(new THREE.Vector3(shakeX, shakeY, shakeZ));
      state.camera.lookAt(playerPos.current.x + 2, 0, playerPos.current.z + 4);
      
      setRenderPos(playerPos.current.clone());
  });

  return (
    <>
      <OrthographicCamera makeDefault position={[-10, 10, -10]} zoom={35} near={-50} far={200} />
      <ambientLight intensity={0.5} />
      <directionalLight position={[-20, 30, 10]} intensity={1.5} castShadow shadow-mapSize={[2048, 2048]} shadow-camera-left={-30} shadow-camera-right={30} shadow-camera-top={30} shadow-camera-bottom={-30} />
      
      <BoundaryWalls playerPos={renderPos} limit={PLAYABLE_WIDTH/2} />
      <WeatherSystem biomeName={currentBiome} playerPos={renderPos} />
      
      <Player position={renderPos} isMoving={visualState.moving} direction={visualState.dir} jumpProgress={visualState.jump} skinId={currentSkin} />
      
      {highScore > 0 && (
          <group position={[0, 0.05, highScore]}>
              <mesh rotation={[-Math.PI/2, 0, 0]}>
                  <planeGeometry args={[PLAYABLE_WIDTH, 0.1]} />
                  <meshBasicMaterial color="#FFD700" opacity={0.5} transparent />
              </mesh>
              <mesh position={[PLAYABLE_WIDTH/2 + 1, 0.5, 0]}>
                   <boxGeometry args={[0.2, 1, 0.2]} />
                   <meshStandardMaterial color="#FFD700" />
              </mesh>
          </group>
      )}

      {lanes.map(lane => (
          <Lane key={lane.id} data={lane} playerPos={renderPos} onCollision={handleDeath} collectedCoins={collectedCoins} triggerShake={(amt) => cameraShake.current = amt} />
      ))}
    </>
  );
};